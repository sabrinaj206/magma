/*
 * Copyright (c) Facebook, Inc. and its affiliates.
 * All rights reserved.
 *
 * This source code is licensed under the BSD-style license found in the
 * LICENSE file in the root directory of this source tree.
 */

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"magma/orc8r/cloud/go/sql_utils"
	"magma/orc8r/cloud/go/storage"

	"github.com/golang/glog"
	"github.com/thoas/go-funk"
)

const (
	networksTable      = "cfg_networks"
	networkConfigTable = "cfg_network_configs"

	entityTable      = "cfg_entities"
	entityAssocTable = "cfg_assocs"
	entityAclTable   = "cfg_acls"
)

const (
	wildcardAllString = "*"
)

// NewSQLConfiguratorStorageFactory returns a ConfiguratorStorageFactory
// implementation backed by a SQL database.
func NewSQLConfiguratorStorageFactory(db *sql.DB) ConfiguratorStorageFactory {
	return &sqlConfiguratorStorageFactory{db}
}

type sqlConfiguratorStorageFactory struct {
	db *sql.DB
}

func (fact *sqlConfiguratorStorageFactory) InitializeServiceStorage() (err error) {
	tx, err := fact.db.BeginTx(context.Background(), &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
	if err != nil {
		return
	}

	defer func() {
		if err == nil {
			err = tx.Commit()
		} else {
			rollbackErr := tx.Rollback()
			if rollbackErr != nil {
				err = fmt.Errorf("%s; rollback error: %s", err, rollbackErr)
			}
		}
	}()

	networksTableExec := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
		(
			id TEXT PRIMARY KEY,
			name TEXT,
			description TEXT,
			version INTEGER NOT NULL DEFAULT 0
		)
	`, networksTable)

	networksConfigTableExec := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
		(
			network_id TEXT REFERENCES %s (id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			value BYTEA,

			PRIMARY KEY (network_id, type)
		)
	`, networkConfigTable, networksTable)

	// Create an internal-only primary key (UUID) for entities.
	// This keeps index size in control and supporting table schemas simpler.
	entityTableExec := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
		(
			pk TEXT PRIMARY KEY,

			network_id TEXT REFERENCES %s (id) ON DELETE CASCADE,
			key TEXT NOT NULL,
			type TEXT NOT NULL,

			graph_id TEXT NOT NULL,

			name TEXT,
			description TEXT,
			physical_id TEXT,
			config BYTEA,

			version INTEGER NOT NULL DEFAULT 0,

			UNIQUE (network_id, key, type)			
		)
	`, entityTable, networksTable)

	entityAssocTableExec := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
		(
			from_pk TEXT REFERENCES %s (pk),
			to_pk TEXT REFERENCES %s (pk),

			PRIMARY KEY (from_pk, to_pk)
		)
	`, entityAssocTable, entityTable, entityTable)

	entityAclTableExec := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
		(
			id TEXT PRIMARY KEY,
			entity_pk TEXT REFERENCES %s (pk) ON DELETE CASCADE,

			scope text NOT NULL,
			permission INTEGER NOT NULL,
			type text NOT NULL,
			id_filter TEXT,

			version INTEGER NOT NULL DEFAULT 0
		)
	`, entityAclTable, entityTable)

	// Named return value for err so we can automatically decide to
	// commit/rollback
	tablesToCreate := []string{
		networksTableExec,
		networksConfigTableExec,
		entityTableExec,
		entityAssocTableExec,
		entityAclTableExec,
	}
	for _, execQuery := range tablesToCreate {
		_, err = tx.Exec(execQuery)
		if err != nil {
			return
		}
	}

	// Create indexes (index is not implicitly created on a referencing FK)
	indexesToCreate := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS graph_id_idx ON %s (graph_id)", entityTable),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS acl_ent_pk_idx ON %s (entity_pk)", entityAclTable),
	}
	for _, execQuery := range indexesToCreate {
		_, err = tx.Exec(execQuery)
		if err != nil {
			return
		}
	}

	// Create internal network(s)
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (id, name, description) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING", networksTable),
		InternalNetworkID, internalNetworkName, internalNetworkDescription,
	)
	if err != nil {
		err = fmt.Errorf("error creating internal networks: %s", err)
		return
	}

	return
}

func (fact *sqlConfiguratorStorageFactory) StartTransaction(ctx context.Context, opts *TxOptions) (ConfiguratorStorage, error) {
	tx, err := fact.db.BeginTx(ctx, getSqlOpts(opts))
	if err != nil {
		return nil, err
	}
	return &sqlConfiguratorStorage{tx: tx}, nil
}

func getSqlOpts(opts *TxOptions) *sql.TxOptions {
	if opts == nil {
		return nil
	}
	return &sql.TxOptions{ReadOnly: opts.ReadOnly}
}

type sqlConfiguratorStorage struct {
	tx *sql.Tx
}

func (store *sqlConfiguratorStorage) Commit() error {
	return store.tx.Commit()
}

func (store *sqlConfiguratorStorage) Rollback() error {
	return store.tx.Rollback()
}

func (store *sqlConfiguratorStorage) LoadNetworks(ids []string, loadCriteria NetworkLoadCriteria) (NetworkLoadResult, error) {
	emptyRet := NetworkLoadResult{NetworkIDsNotFound: []string{}, Networks: []Network{}}
	if len(ids) == 0 {
		return emptyRet, nil
	}

	query, err := getNetworkQuery(ids, loadCriteria)
	if err != nil {
		return emptyRet, err
	}
	queryArgs := make([]interface{}, 0, len(ids))
	funk.ConvertSlice(ids, &queryArgs)

	rows, err := store.tx.Query(query, queryArgs...)
	if err != nil {
		return emptyRet, fmt.Errorf("error querying for networks: %s", err)
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			glog.Warningf("error while closing *Rows object in LoadNetworks: %s", err)
		}
	}()

	// Pointer values because we're modifying .Config in-place
	loadedNetworksByID := map[string]*Network{}
	for rows.Next() {
		nwResult, err := scanNextNetworkRow(rows, loadCriteria)
		if err != nil {
			return emptyRet, err
		}

		existingNetwork, wasLoaded := loadedNetworksByID[nwResult.ID]
		if wasLoaded {
			for k, v := range nwResult.Configs {
				existingNetwork.Configs[k] = v
			}
		} else {
			loadedNetworksByID[nwResult.ID] = &nwResult
		}
	}

	// Sort map keys so we return deterministically
	loadedNetworkIDs := funk.Keys(loadedNetworksByID).([]string)
	sort.Strings(loadedNetworkIDs)

	ret := NetworkLoadResult{
		NetworkIDsNotFound: getNetworkIDsNotFound(loadedNetworksByID, ids),
		Networks:           make([]Network, 0, len(loadedNetworksByID)),
	}
	for _, nid := range loadedNetworkIDs {
		ret.Networks = append(ret.Networks, *loadedNetworksByID[nid])
	}
	return ret, nil
}

func (store *sqlConfiguratorStorage) CreateNetwork(network Network) (Network, error) {
	exists, err := store.doesNetworkExist(network.ID)
	if err != nil {
		return network, err
	}
	if exists {
		return network, fmt.Errorf("a network with ID %s already exists", network.ID)
	}

	// Insert the network, then insert its configs
	exec := fmt.Sprintf("INSERT INTO %s (id, name, description) VALUES ($1, $2, $3)", networksTable)
	_, err = store.tx.Exec(exec, network.ID, network.Name, network.Description)
	if err != nil {
		return network, fmt.Errorf("error inserting network: %s", err)
	}

	if funk.IsEmpty(network.Configs) {
		return network, nil
	}

	configExec := fmt.Sprintf("INSERT INTO %s (network_id, type, value) VALUES ($1, $2, $3)", networkConfigTable)
	configInsertStatement, err := store.tx.Prepare(configExec)
	if err != nil {
		return network, fmt.Errorf("error preparing network configuration insert: %s", err)
	}
	defer sql_utils.GetCloseStatementsDeferFunc([]*sql.Stmt{configInsertStatement}, "CreateNetwork")()

	// Sort config keys for deterministic behavior
	configKeys := funk.Keys(network.Configs).([]string)
	sort.Strings(configKeys)
	for _, configKey := range configKeys {
		_, err := configInsertStatement.Exec(network.ID, configKey, network.Configs[configKey])
		if err != nil {
			return network, fmt.Errorf("error inserting config %s: %s", configKey, err)
		}
	}

	return network, nil
}

func (store *sqlConfiguratorStorage) UpdateNetworks(updates []NetworkUpdateCriteria) (FailedOperations, error) {
	failures := FailedOperations{}
	if err := validateNetworkUpdates(updates); err != nil {
		return failures, err
	}

	// Prepare statements
	deleteExec := fmt.Sprintf("DELETE FROM %s WHERE id = $1", networksTable)
	upsertConfigExec := fmt.Sprintf(`
		INSERT INTO %s (network_id, type, value) VALUES ($1, $2, $3)
		ON CONFLICT (network_id, type) DO UPDATE SET value = $4
	`, networkConfigTable)
	deleteConfigExec := fmt.Sprintf("DELETE FROM %s WHERE (network_id, type) = ($1, $2)", networkConfigTable)
	statements, err := sql_utils.PrepareStatements(store.tx, []string{deleteExec, upsertConfigExec, deleteConfigExec})
	if err != nil {
		return failures, err
	}
	defer sql_utils.GetCloseStatementsDeferFunc(statements, "UpdateNetworks")()

	deleteStmt, upsertConfigStmt, deleteConfigStmt := statements[0], statements[1], statements[2]
	for _, update := range updates {
		if update.DeleteNetwork {
			_, err := deleteStmt.Exec(update.ID)
			if err != nil {
				failures[update.ID] = fmt.Errorf("error deleting network %s: %s", update.ID, err)
			}
		} else {
			err := store.updateNetwork(update, upsertConfigStmt, deleteConfigStmt)
			if err != nil {
				failures[update.ID] = err
			}
		}
	}

	if funk.IsEmpty(failures) {
		return failures, nil
	}
	return failures, errors.New("some errors were encountered, see return value for details")
}

func (store *sqlConfiguratorStorage) LoadEntities(networkID string, filter EntityLoadFilter, loadCriteria EntityLoadCriteria) (EntityLoadResult, error) {
	ret := EntityLoadResult{Entities: []NetworkEntity{}, EntitiesNotFound: []storage.TypeAndKey{}}

	// We load the requested entities in 3 steps:
	// First, we load the entities and their ACLs
	// Then, we load assocs if requested by the load criteria. Note that the
	// load criteria can specify to load edges to and/or from the requested
	// entities.
	// For each loaded edge, we need to load the (type, key) corresponding to
	// to the PK pair that an edge is represented as. These may be already
	// loaded as part of the first load from the entities table, so we can
	// be smart here and only load (type, key) for PKs which we don't know.
	// Finally, we will update the entity objects to return with their edges.

	entsByPk, err := store.loadFromEntitiesTable(networkID, filter, loadCriteria)
	if err != nil {
		return ret, err
	}
	assocs, allAssocPks, err := store.loadFromAssocsTable(filter, loadCriteria, entsByPk)
	if err != nil {
		return ret, err
	}
	entTksByPk, err := store.loadEntityTypeAndKeys(allAssocPks, entsByPk)
	if err != nil {
		return ret, err
	}

	entsByPk, err = updateEntitiesWithAssocs(entsByPk, assocs, entTksByPk, loadCriteria)
	if err != nil {
		return ret, err
	}

	for _, ent := range entsByPk {
		ret.Entities = append(ret.Entities, *ent)
	}
	ret.EntitiesNotFound = calculateEntitiesNotFound(entsByPk, filter.IDs)

	// Sort entities for deterministic returns
	entComparator := func(a, b NetworkEntity) bool {
		return storage.TypeAndKey{Type: a.Type, Key: a.Key}.String() < storage.TypeAndKey{Type: b.Type, Key: b.Key}.String()
	}
	sort.Slice(ret.Entities, func(i, j int) bool { return entComparator(ret.Entities[i], ret.Entities[j]) })

	return ret, nil
}

func (store *sqlConfiguratorStorage) CreateEntities(networkID string, entities []NetworkEntity) (EntityCreationResult, error) {
	panic("implement me")
}

func (store *sqlConfiguratorStorage) UpdateEntities(networkID string, updates []EntityUpdateCriteria) (FailedOperations, error) {
	panic("implement me")
}

func (store *sqlConfiguratorStorage) LoadGraphForEntity(networkID string, entityID storage.TypeAndKey, loadCriteria EntityLoadCriteria) (EntityGraph, error) {
	panic("implement me")
}