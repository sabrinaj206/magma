"""
Copyright (c) 2016-present, Facebook, Inc.
All rights reserved.

This source code is licensed under the BSD-style license found in the
LICENSE file in the root directory of this source tree. An additional grant
of patent rights can be found in the PATENTS file in the same directory.
"""

import asyncio
import logging
from typing import Any, List

from magma.common.service import MagmaService
from magma.common.streamer import StreamerClient
from magma.configuration.mconfig_managers import MconfigManager, \
    load_service_mconfig
from magma.magmad.service_manager import ServiceManager

CONFIG_STREAM_NAME = 'configs'


class ConfigManager(StreamerClient.Callback):
    """
    Manager for access gateway config. Updates are received as a stream and
    are guaranteed to be lossless and in-order. Config is written to file in
    JSON format.
    """

    def __init__(self, services: List[str], service_manager: ServiceManager,
                 magmad_service: MagmaService, mconfig_manager: MconfigManager,
                 allow_unknown_fields: bool = True, loop=None) -> None:
        """
        Args:
            services: List of services to manage
            service_manager: ServiceManager instance
            magmad_service: magmad service instance
            mconfig_manager: manager class for the mconfig
            allow_unknown_fields: set to True to suppress unknown field errors
            loop: asyncio event loop to run in
        """
        self._services = services
        self._service_manager = service_manager
        self._magmad_service = magmad_service
        self._mconfig_manager = mconfig_manager
        self._allow_unknown_fields = allow_unknown_fields
        self._loop = loop or asyncio.get_event_loop()

        # Load managed config
        self._mconfig = self._mconfig_manager.load_mconfig()

    def get_request_args(self, stream_name: str) -> Any:
        return None

    def process_update(self, stream_name, updates, resync):
        """
        Handle config updates. Resync is ignored since the entire config
        structure is passed in every update.
        Inputs:
         - updates - list of GatewayConfigs protobuf structures
         - resync - boolean indicating whether all database information will be
                    resent (hence cached data can be discarded). This is ignored
                    since config is contained in one DB element, hence all
                    data is sent in every update.
        """
        if len(updates) == 0:
            logging.info('No config update to process')
            return

        # We will only take the last update
        for update in updates[:-1]:
            logging.info('Ignoring config update %s', update.key)

        # Deserialize and store the last config update
        logging.info('Processing config update %s', updates[-1].key)
        mconfig_str = updates[-1].value.decode()
        mconfig = self._mconfig_manager.deserialize_mconfig(
            mconfig_str,
            self._allow_unknown_fields,
        )

        if 'magmad' not in mconfig.configs_by_key:
            logging.error('Invalid config! Magmad service config missing')
            return

        self._mconfig_manager.update_stored_mconfig(mconfig_str)
        self._magmad_service.reload_mconfig()

        def did_mconfig_change(serv_name):
            return mconfig.configs_by_key.get(serv_name) != \
                   self._mconfig.configs_by_key.get(serv_name)

        # Reload magmad configs locally
        if did_mconfig_change('magmad'):
            self._loop.create_task(
                self._service_manager.update_dynamic_services(
                    load_service_mconfig('magmad').dynamic_services,
                )
            )

        services_to_restart = [
            srv for srv in self._services if did_mconfig_change(srv)
        ]
        if services_to_restart:
            self._loop.create_task(
                self._service_manager.restart_services(services_to_restart),
            )

        self._mconfig = mconfig
