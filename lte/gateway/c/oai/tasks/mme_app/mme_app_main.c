/*
 * Licensed to the OpenAirInterface (OAI) Software Alliance under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The OpenAirInterface Software Alliance licenses this file to You under
 * the Apache License, Version 2.0  (the "License"); you may not use this file
 * except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *-------------------------------------------------------------------------------
 * For more information about the OpenAirInterface (OAI) Software Alliance:
 *      contact@openairinterface.org
 */

/*! \file mme_app_main.c
  \brief
  \author Sebastien ROUX, Lionel Gauthier
  \company Eurecom
  \email: lionel.gauthier@eurecom.fr
*/

#include <stdio.h>
#include <string.h>
#include <stdbool.h>
#include <stdint.h>
#include <pthread.h>

#include "bstrlib.h"
#include "dynamic_memory_check.h"
#include "log.h"
#include "assertions.h"
#include "intertask_interface.h"
#include "itti_free_defined_msg.h"
#include "mme_config.h"
#include "timer.h"
#include "mme_app_extern.h"
#include "mme_app_ue_context.h"
#include "mme_app_defs.h"
#include "mme_app_statistics.h"
#include "service303_message_utils.h"
#include "s6a_message_utils.h"
#include "service303.h"
#include "common_defs.h"
#include "mme_app_edns_emulation.h"
#include "nas_proc.h"
#include "3gpp_36.401.h"
#include "common_types.h"
#include "hashtable.h"
#include "intertask_interface_types.h"
#include "itti_types.h"
#include "mme_app_desc.h"
#include "mme_app_messages_types.h"
#include "nas_messages_types.h"
#include "obj_hashtable.h"
#include "s11_messages_types.h"
#include "s1ap_messages_types.h"
#include "sctp_messages_types.h"
#include "timer_messages_types.h"

mme_app_desc_t mme_app_desc = {.rw_lock = PTHREAD_RWLOCK_INITIALIZER, 0};

bool mme_hss_associated = false;
bool mme_sctp_bounded = false;

void *mme_app_thread(void *args);
static void _check_mme_healthy_and_notify_service(void);
static bool _is_mme_app_healthy(void);

//------------------------------------------------------------------------------
void *mme_app_thread(void *args)
{
  struct ue_mm_context_s *ue_context_p = NULL;
  itti_mark_task_ready(TASK_MME_APP);

  while (1) {
    MessageDef *received_message_p = NULL;

    /*
     * Trying to fetch a message from the message queue.
     * If the queue is empty, this function will block till a
     * message is sent to the task.
     */
    itti_receive_msg(TASK_MME_APP, &received_message_p);
    DevAssert(received_message_p);

    switch (ITTI_MSG_ID(received_message_p)) {
      case MESSAGE_TEST: {
        OAI_FPRINTF_INFO("TASK_MME_APP received MESSAGE_TEST\n");
      } break;

      case MME_APP_INITIAL_CONTEXT_SETUP_RSP: {
        mme_app_handle_initial_context_setup_rsp(
          &MME_APP_INITIAL_CONTEXT_SETUP_RSP(received_message_p));
      } break;

      case MME_APP_CREATE_DEDICATED_BEARER_RSP: {
        mme_app_handle_create_dedicated_bearer_rsp(
          &MME_APP_CREATE_DEDICATED_BEARER_RSP(received_message_p));
      } break;

      case MME_APP_CREATE_DEDICATED_BEARER_REJ: {
        mme_app_handle_create_dedicated_bearer_rej(
          &MME_APP_CREATE_DEDICATED_BEARER_REJ(received_message_p));
      } break;

      case NAS_CONNECTION_ESTABLISHMENT_CNF: {
        mme_app_handle_conn_est_cnf(
          &NAS_CONNECTION_ESTABLISHMENT_CNF(received_message_p));
      } break;

      case NAS_DETACH_REQ: {
        mme_app_handle_detach_req(&received_message_p->ittiMsg.nas_detach_req);
      } break;

      case S6A_CANCEL_LOCATION_REQ: {
        /*
         * Check cancellation-type and handle it if it is SUBSCRIPTION_WITHDRAWAL.
         * For any other cancellation-type log it and ignore it.
         */
        mme_app_handle_s6a_cancel_location_req(
          &received_message_p->ittiMsg.s6a_cancel_location_req);
      } break;
      case NAS_ERAB_SETUP_REQ: {
        mme_app_handle_erab_setup_req(&NAS_ERAB_SETUP_REQ(received_message_p));
      } break;

      case NAS_PDN_CONFIG_REQ: {
        OAILOG_INFO(
          TASK_MME_APP, "Received PDN CONFIG REQ from NAS_MME for ue_id = (%u)\n",
          received_message_p->ittiMsg.nas_pdn_config_req.ue_id);
        struct ue_mm_context_s *ue_context_p = NULL;
        ue_context_p = mme_ue_context_exists_mme_ue_s1ap_id(
          &mme_app_desc.mme_ue_contexts,
          received_message_p->ittiMsg.nas_pdn_config_req.ue_id);
        if (ue_context_p) {
          mme_app_send_s6a_update_location_req(ue_context_p);
          unlock_ue_contexts(ue_context_p);
        } else {
          OAILOG_ERROR(
            TASK_MME_APP, "UE context NULL for ue_id = (%u)\n",
            received_message_p->ittiMsg.nas_pdn_config_req.ue_id);
        }
      } break;

      case NAS_PDN_CONNECTIVITY_REQ: {
        OAILOG_INFO(
          TASK_MME_APP, "Received PDN CONNECTIVITY REQ from NAS_MME\n");
        mme_app_handle_nas_pdn_connectivity_req(
          &received_message_p->ittiMsg.nas_pdn_connectivity_req);
      } break;

      case NAS_UPLINK_DATA_IND: {
        ue_context_p = mme_ue_context_exists_mme_ue_s1ap_id(
          &mme_app_desc.mme_ue_contexts,
          NAS_UL_DATA_IND(received_message_p).ue_id);
        nas_proc_ul_transfer_ind(
          NAS_UL_DATA_IND(received_message_p).ue_id,
          NAS_UL_DATA_IND(received_message_p).tai,
          NAS_UL_DATA_IND(received_message_p).cgi,
          &NAS_UL_DATA_IND(received_message_p).nas_msg);
        if (ue_context_p) {
          unlock_ue_contexts(ue_context_p);
        }
      } break;

      case S11_CREATE_BEARER_REQUEST: {
        mme_app_handle_s11_create_bearer_req(
          &received_message_p->ittiMsg.s11_create_bearer_request);
      } break;

      case S6A_RESET_REQ: {
        mme_app_handle_s6a_reset_req(
          &received_message_p->ittiMsg.s6a_reset_req);
      } break;

      case S11_CREATE_SESSION_RESPONSE: {
        mme_app_handle_create_sess_resp(
          &received_message_p->ittiMsg.s11_create_session_response);
      } break;

      case S11_MODIFY_BEARER_RESPONSE: {
        OAILOG_INFO(
          TASK_MME_APP, "Received S11 MODIFY BEARER RESPONSE from SPGW\n");
        ue_context_p = mme_ue_context_exists_s11_teid(
          &mme_app_desc.mme_ue_contexts,
          received_message_p->ittiMsg.s11_modify_bearer_response.teid);

        if (ue_context_p == NULL) {
          OAILOG_WARNING(
            LOG_MME_APP,
            "We didn't find this teid in list of UE: %08x\n",
            received_message_p->ittiMsg.s11_modify_bearer_response.teid);
        } else {
          OAILOG_DEBUG(
            TASK_MME_APP, "S11 MODIFY BEARER RESPONSE local S11 teid = " TEID_FMT"\n",
            received_message_p->ittiMsg.s11_modify_bearer_response.teid);
          /*
           * Updating statistics
           */
          update_mme_app_stats_s1u_bearer_add();
          unlock_ue_contexts(ue_context_p);
        }
      } break;

      case S11_RELEASE_ACCESS_BEARERS_RESPONSE: {
        mme_app_handle_release_access_bearers_resp(
          &received_message_p->ittiMsg.s11_release_access_bearers_response);
      } break;

      case S11_DELETE_SESSION_RESPONSE: {
        mme_app_handle_delete_session_rsp(
          &received_message_p->ittiMsg.s11_delete_session_response);
      } break;

      case S11_SUSPEND_ACKNOWLEDGE: {
        mme_app_handle_suspend_acknowledge(
          &received_message_p->ittiMsg.s11_suspend_acknowledge);
      } break;

      case S1AP_E_RAB_SETUP_RSP: {
        mme_app_handle_e_rab_setup_rsp(
          &S1AP_E_RAB_SETUP_RSP(received_message_p));
      } break;

      case NAS_EXTENDED_SERVICE_REQ: {
        mme_app_handle_nas_extended_service_req(
          &received_message_p->ittiMsg.nas_extended_service_req);
      } break;

      case S1AP_INITIAL_UE_MESSAGE: {
        mme_app_handle_initial_ue_message(
          &S1AP_INITIAL_UE_MESSAGE(received_message_p));
      } break;

      case NAS_SGS_DETACH_REQ: {
        OAILOG_INFO(LOG_MME_APP, "Recieved SGS detach request from NAS\n");
        mme_app_handle_sgs_detach_req(
          &received_message_p->ittiMsg.nas_sgs_detach_req);
      } break;

      case S6A_UPDATE_LOCATION_ANS: {
        /*
         * We received the update location answer message from HSS -> Handle it
         */
        OAILOG_INFO(LOG_MME_APP, "Received S6A Update Location Answer from S6A\n");
        mme_app_handle_s6a_update_location_ans(
          &received_message_p->ittiMsg.s6a_update_location_ans);
      } break;

      case S1AP_ENB_INITIATED_RESET_REQ: {
        mme_app_handle_enb_reset_req(
          &S1AP_ENB_INITIATED_RESET_REQ(received_message_p));
      } break;

      case S11_PAGING_REQUEST: {
        const char *imsi = received_message_p->ittiMsg.s11_paging_request.imsi;
        OAILOG_DEBUG(
          TASK_MME_APP, "MME handling paging request for IMSI%s\n", imsi);
        if (mme_app_handle_initial_paging_request(imsi) != RETURNok) {
          OAILOG_ERROR(
            TASK_MME_APP,
            "Failed to send paging request to S1AP for IMSI%s\n",
            imsi);
        }
      } break;

      case MME_APP_INITIAL_CONTEXT_SETUP_FAILURE: {
        mme_app_handle_initial_context_setup_failure(
          &MME_APP_INITIAL_CONTEXT_SETUP_FAILURE(received_message_p));
      } break;

      case TIMER_HAS_EXPIRED: {
        /*
         * Check statistic timer
         */
        if (!timer_exists(
              received_message_p->ittiMsg.timer_has_expired.timer_id)) {
          OAILOG_WARNING(
            LOG_MME_APP,
            "Timer expiry signal received for timer \
            %lu, but it has already been deleted\n",
            received_message_p->ittiMsg.timer_has_expired.timer_id);
          break;
        }
        if (
          received_message_p->ittiMsg.timer_has_expired.timer_id ==
          mme_app_desc.statistic_timer_id) {
          mme_app_statistics_display();
        } else if (received_message_p->ittiMsg.timer_has_expired.arg != NULL) {
          mme_ue_s1ap_id_t mme_ue_s1ap_id =
            *((mme_ue_s1ap_id_t *) (received_message_p->ittiMsg
                                      .timer_has_expired.arg));
          ue_context_p = mme_ue_context_exists_mme_ue_s1ap_id(
            &mme_app_desc.mme_ue_contexts, mme_ue_s1ap_id);
          if (ue_context_p == NULL) {
            OAILOG_WARNING(
              LOG_MME_APP,
              "Timer expired but no assoicated UE context for UE "
              "id " MME_UE_S1AP_ID_FMT "\n",
              mme_ue_s1ap_id);
            timer_handle_expired(
              received_message_p->ittiMsg.timer_has_expired.timer_id);
            break;
          }
          if (
            received_message_p->ittiMsg.timer_has_expired.timer_id ==
            ue_context_p->mobile_reachability_timer.id) {
            // Mobile Reachability Timer expiry handler
            mme_app_handle_mobile_reachability_timer_expiry(ue_context_p);
          } else if (
            received_message_p->ittiMsg.timer_has_expired.timer_id ==
            ue_context_p->implicit_detach_timer.id) {
            // Implicit Detach Timer expiry handler
            increment_counter("implicit_detach_timer_expired", 1, NO_LABELS);
            mme_app_handle_implicit_detach_timer_expiry(ue_context_p);
          } else if (
            received_message_p->ittiMsg.timer_has_expired.timer_id ==
            ue_context_p->initial_context_setup_rsp_timer.id) {
            // Initial Context Setup Rsp Timer expiry handler
            increment_counter(
              "initial_context_setup_request_timer_expired", 1, NO_LABELS);
            mme_app_handle_initial_context_setup_rsp_timer_expiry(ue_context_p);
          } else if (
            received_message_p->ittiMsg.timer_has_expired.timer_id ==
            ue_context_p->paging_response_timer.id) {
            mme_app_handle_paging_timer_expiry(ue_context_p);
          } else if (
            received_message_p->ittiMsg.timer_has_expired.timer_id ==
            ue_context_p->ulr_response_timer.id) {
            mme_app_handle_ulr_timer_expiry(ue_context_p);
          } else if (
            received_message_p->ittiMsg.timer_has_expired.timer_id ==
            ue_context_p->ue_context_modification_timer.id) {
            // UE Context modification Timer expiry handler
            increment_counter(
              "ue_context_modification_timer expired", 1, NO_LABELS);
            mme_app_handle_ue_context_modification_timer_expiry(ue_context_p);
          } else if (ue_context_p->sgs_context != NULL){
              if (received_message_p->ittiMsg.timer_has_expired.timer_id ==
                  ue_context_p->sgs_context->ts6_1_timer.id) {
                  mme_app_handle_ts6_1_timer_expiry(ue_context_p);
              } else if (received_message_p->ittiMsg.timer_has_expired.timer_id ==
                ue_context_p->sgs_context->ts8_timer.id) {
                mme_app_handle_sgs_eps_detach_timer_expiry(ue_context_p);
              } else if (received_message_p->ittiMsg.timer_has_expired.timer_id ==
                ue_context_p->sgs_context->ts9_timer.id) {
                mme_app_handle_sgs_imsi_detach_timer_expiry(ue_context_p);
              } else if (received_message_p->ittiMsg.timer_has_expired.timer_id ==
                ue_context_p->sgs_context->ts10_timer.id) {
                mme_app_handle_sgs_implicit_imsi_detach_timer_expiry(ue_context_p);
              } else if (received_message_p->ittiMsg.timer_has_expired.timer_id ==
                ue_context_p->sgs_context->ts13_timer.id) {
                mme_app_handle_sgs_implicit_eps_detach_timer_expiry(ue_context_p);
              }
          }
          else {
            OAILOG_WARNING(
              LOG_MME_APP,
              "Timer expired but no associated timer_id for UE "
              "id " MME_UE_S1AP_ID_FMT "\n",
              mme_ue_s1ap_id);
          }
          if (ue_context_p) {
            unlock_ue_contexts(ue_context_p);
          }
        }
        timer_handle_expired(
          received_message_p->ittiMsg.timer_has_expired.timer_id);
      } break;

      case S1AP_UE_CAPABILITIES_IND: {
        mme_app_handle_s1ap_ue_capabilities_ind(
          &received_message_p->ittiMsg.s1ap_ue_cap_ind);
      } break;

      case S1AP_UE_CONTEXT_RELEASE_REQ: {
        mme_app_handle_s1ap_ue_context_release_req(
          &received_message_p->ittiMsg.s1ap_ue_context_release_req);
      } break;

      case S1AP_UE_CONTEXT_MODIFICATION_RESPONSE: {
        mme_app_handle_s1ap_ue_context_modification_resp(
          &received_message_p->ittiMsg.s1ap_ue_context_mod_response);
      } break;

      case S1AP_UE_CONTEXT_MODIFICATION_FAILURE: {
        mme_app_handle_s1ap_ue_context_modification_fail(
          &received_message_p->ittiMsg.s1ap_ue_context_mod_failure);
      } break;
      case S1AP_UE_CONTEXT_RELEASE_COMPLETE: {
        mme_app_handle_s1ap_ue_context_release_complete(
          &received_message_p->ittiMsg.s1ap_ue_context_release_complete);
      } break;

      case NAS_DOWNLINK_DATA_REQ: {
        mme_app_handle_nas_dl_req(&received_message_p->ittiMsg.nas_dl_data_req);
      } break;

      case S1AP_ENB_DEREGISTERED_IND: {
        mme_app_handle_enb_deregister_ind(
          &received_message_p->ittiMsg.s1ap_eNB_deregistered_ind);
      } break;

      case ACTIVATE_MESSAGE: {
        mme_hss_associated = true;
        _check_mme_healthy_and_notify_service();
      } break;

      case SCTP_MME_SERVER_INITIALIZED: {
        mme_sctp_bounded =
          &received_message_p->ittiMsg.sctp_mme_server_initialized.successful;
        _check_mme_healthy_and_notify_service();
      } break;

      case S6A_PURGE_UE_ANS: {
        mme_app_handle_s6a_purge_ue_ans(
          &received_message_p->ittiMsg.s6a_purge_ue_ans);
      } break;

      case NAS_CS_DOMAIN_LOCATION_UPDATE_REQ: {
        /*Received SGS Location Update Request message from NAS task*/
        OAILOG_INFO(
          TASK_MME_APP, "Received CS DOMAIN LOCATION UPDATE REQ from NAS\n");
        mme_app_handle_nas_cs_domain_location_update_req(
          &received_message_p->ittiMsg.nas_cs_domain_location_update_req);
      } break;

      case SGSAP_LOCATION_UPDATE_ACC: {
        /*Received SGSAP Location Update Accept message from SGS task*/
        OAILOG_INFO(
          TASK_MME_APP, "Received SGSAP Location Update Accept from SGS\n");
        mme_app_handle_sgsap_location_update_acc(
          &received_message_p->ittiMsg.sgsap_location_update_acc);
      } break;

      case SGSAP_LOCATION_UPDATE_REJ: {
        /*Received SGSAP Location Update Reject message from SGS task*/
        mme_app_handle_sgsap_location_update_rej(
          &received_message_p->ittiMsg.sgsap_location_update_rej);
      } break;

      case NAS_TAU_COMPLETE: {
        /*Received TAU Complete message from NAS task*/
        mme_app_handle_nas_tau_complete(
          &received_message_p->ittiMsg.nas_tau_complete);
      } break;

      case SGSAP_ALERT_REQUEST: {
        /*Received SGSAP Alert Request message from SGS task*/
        mme_app_handle_sgsap_alert_request(
          &received_message_p->ittiMsg.sgsap_alert_request);
      } break;

      case SGSAP_VLR_RESET_INDICATION: {
        /*Received SGSAP Reset Indication from SGS task*/
        mme_app_handle_sgsap_reset_indication(
          &received_message_p->ittiMsg.sgsap_vlr_reset_indication);
      } break;

      case SGSAP_PAGING_REQUEST: {
        mme_app_handle_sgsap_paging_request(
          &received_message_p->ittiMsg.sgsap_paging_request);
      } break;

      case SGSAP_SERVICE_ABORT_REQ: {
        mme_app_handle_sgsap_service_abort_request(
          &received_message_p->ittiMsg.sgsap_service_abort_req);
      } break;

      case SGSAP_EPS_DETACH_ACK: {
        mme_app_handle_sgs_eps_detach_ack(
          &received_message_p->ittiMsg.sgsap_eps_detach_ack);
      } break;

      case SGSAP_IMSI_DETACH_ACK: {
        mme_app_handle_sgs_imsi_detach_ack(
          &received_message_p->ittiMsg.sgsap_imsi_detach_ack);
      } break;

      case S11_MODIFY_UE_AMBR_REQUEST: {
        mme_app_handle_modify_ue_ambr_request(
          &S11_MODIFY_UE_AMBR_REQUEST(received_message_p));
      } break;

      case SGSAP_STATUS: {
        mme_app_handle_sgs_status_message(
          &received_message_p->ittiMsg.sgsap_status);
      } break;

      case TERMINATE_MESSAGE: {
        /*
       * Termination message received TODO -> release any data allocated
       */
        mme_app_exit();
        itti_free_msg_content(received_message_p);
        itti_free(ITTI_MSG_ORIGIN_ID(received_message_p), received_message_p);
        OAI_FPRINTF_INFO("TASK_MME_APP terminated\n");
        itti_exit_task();
      } break;

      default: {
        OAILOG_DEBUG(
          LOG_MME_APP,
          "Unkwnon message ID %d:%s\n",
          ITTI_MSG_ID(received_message_p),
          ITTI_MSG_NAME(received_message_p));
        AssertFatal(
          0,
          "Unkwnon message ID %d:%s\n",
          ITTI_MSG_ID(received_message_p),
          ITTI_MSG_NAME(received_message_p));
      } break;
    }

    itti_free_msg_content(received_message_p);
    itti_free(ITTI_MSG_ORIGIN_ID(received_message_p), received_message_p);
    received_message_p = NULL;
  }

  return NULL;
}

//------------------------------------------------------------------------------
int mme_app_init(const mme_config_t *mme_config_p)
{
  OAILOG_FUNC_IN(LOG_MME_APP);
  memset(&mme_app_desc, 0, sizeof(mme_app_desc));
  pthread_rwlock_init(&mme_app_desc.rw_lock, NULL);
  bstring b = bfromcstr("mme_app_imsi_ue_context_htbl");
  mme_app_desc.mme_ue_contexts.imsi_ue_context_htbl =
    hashtable_uint64_ts_create(mme_config.max_ues, NULL, b);
  btrunc(b, 0);
  bassigncstr(b, "mme_app_tun11_ue_context_htbl");
  mme_app_desc.mme_ue_contexts.tun11_ue_context_htbl =
    hashtable_uint64_ts_create(mme_config.max_ues, NULL, b);
  AssertFatal(
    sizeof(uintptr_t) >= sizeof(uint64_t),
    "Problem with mme_ue_s1ap_id_ue_context_htbl in MME_APP");
  btrunc(b, 0);
  bassigncstr(b, "mme_app_mme_ue_s1ap_id_ue_context_htbl");
  mme_app_desc.mme_ue_contexts.mme_ue_s1ap_id_ue_context_htbl =
    hashtable_ts_create(mme_config.max_ues, NULL, NULL, b);
  btrunc(b, 0);
  bassigncstr(b, "mme_app_enb_ue_s1ap_id_ue_context_htbl");
  mme_app_desc.mme_ue_contexts.enb_ue_s1ap_id_ue_context_htbl =
    hashtable_uint64_ts_create(mme_config.max_ues, NULL, b);
  btrunc(b, 0);
  bassigncstr(b, "mme_app_guti_ue_context_htbl");
  mme_app_desc.mme_ue_contexts.guti_ue_context_htbl =
    obj_hashtable_uint64_ts_create(mme_config.max_ues, NULL, NULL, b);
  bdestroy_wrapper(&b);

  if (mme_app_edns_init(mme_config_p)) {
    OAILOG_FUNC_RETURN(LOG_MME_APP, RETURNerror);
  }
  /*
   * Create the thread associated with MME applicative layer
   */
  if (itti_create_task(TASK_MME_APP, &mme_app_thread, NULL) < 0) {
    OAILOG_ERROR(LOG_MME_APP, "MME APP create task failed\n");
    OAILOG_FUNC_RETURN(LOG_MME_APP, RETURNerror);
  }

  mme_app_desc.statistic_timer_period = mme_config_p->mme_statistic_timer;

  /*
   * Request for periodic timer
   */
  if (
    timer_setup(
      mme_config_p->mme_statistic_timer,
      0,
      TASK_MME_APP,
      INSTANCE_DEFAULT,
      TIMER_PERIODIC,
      NULL,
      0,
      &mme_app_desc.statistic_timer_id) < 0) {
    OAILOG_ERROR(
      LOG_MME_APP,
      "Failed to request new timer for statistics with %ds "
      "of periocidity\n",
      mme_config_p->mme_statistic_timer);
    mme_app_desc.statistic_timer_id = 0;
  }

  OAILOG_DEBUG(LOG_MME_APP, "Initializing MME applicative layer: DONE\n");
  OAILOG_FUNC_RETURN(LOG_MME_APP, RETURNok);
}

static void _check_mme_healthy_and_notify_service(void)
{
  if (_is_mme_app_healthy()) {
    send_app_health_to_service303(TASK_MME_APP, true);
    send_start_s6a_server(TASK_MME_APP);
  }
}

static bool _is_mme_app_healthy(void)
{
  return mme_hss_associated && mme_sctp_bounded;
}

//------------------------------------------------------------------------------
void mme_app_exit(void)
{
  timer_remove(mme_app_desc.statistic_timer_id, NULL);
  mme_app_edns_exit();
  hashtable_uint64_ts_destroy(
    mme_app_desc.mme_ue_contexts.imsi_ue_context_htbl);
  hashtable_uint64_ts_destroy(
    mme_app_desc.mme_ue_contexts.tun11_ue_context_htbl);
  hashtable_ts_destroy(
    mme_app_desc.mme_ue_contexts.mme_ue_s1ap_id_ue_context_htbl);
  hashtable_uint64_ts_destroy(
    mme_app_desc.mme_ue_contexts.enb_ue_s1ap_id_ue_context_htbl);
  obj_hashtable_uint64_ts_destroy(
    mme_app_desc.mme_ue_contexts.guti_ue_context_htbl);
  mme_config_exit();
}
