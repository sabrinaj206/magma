/*
Copyright (c) Facebook, Inc. and its affiliates.
All rights reserved.

This source code is licensed under the BSD-style license found in the
LICENSE file in the root directory of this source tree.
*/

// Package servicers implements Swx GRPC proxy service which sends MAR/SAR messages over
// diameter connection, waits (blocks) for diameter's MAA/SAAs and returns their RPC representation
package servicers

import (
	"fmt"
	"strconv"
	"time"

	"magma/feg/cloud/go/protos"
	"magma/feg/gateway/diameter"
	"magma/feg/gateway/services/swx_proxy/metrics"

	"github.com/fiorix/go-diameter/diam"
	"github.com/fiorix/go-diameter/diam/avp"
	"github.com/fiorix/go-diameter/diam/datatype"
	"github.com/fiorix/go-diameter/diam/dict"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MinRequestedVectors uint32 = 5

// AuthenticateImpl sends MAR over diameter connection,
// waits (blocks) for MAA & returns its RPC representation
func (s *swxProxy) AuthenticateImpl(req *protos.AuthenticationRequest) (*protos.AuthenticationAnswer, error) {
	var (
		res = &protos.AuthenticationAnswer{}
		err = validateAuthRequest(req)
	)
	if err != nil {
		return res, status.Errorf(codes.InvalidArgument, err.Error())
	}
	res.UserName = req.GetUserName()
	shouldSendSar := s.config.VerifyAuthorization || req.GetRetrieveUserProfile()

	if s.cache != nil {
		// Check if we still have valid vectors for the user in the cache
		if len(req.GetResyncInfo()) == 0 { // Only try to get cached vectors if it's not resync request
			cachedRes := s.cache.Get(res.UserName)
			if cachedRes != nil && ((!shouldSendSar) || cachedRes.GetUserProfile() != nil) {
				return cachedRes, nil // We have a valid result in the cache, return it
			}
		}

		if req.SipNumAuthVectors < MinRequestedVectors {
			req.SipNumAuthVectors = MinRequestedVectors // Get Max allowed # of vectors for caching
		}
	}

	sid := s.genSID()
	ch := make(chan interface{})
	s.requestTracker.RegisterRequest(sid, ch)
	// if request hasn't been removed by end of transaction, remove it
	defer s.requestTracker.DeregisterRequest(sid)

	marMsg, err := s.createMAR(sid, req)
	if err != nil {
		return res, status.Errorf(codes.InvalidArgument, err.Error())
	}
	err = s.sendDiameterMsg(marMsg, MAX_DIAM_RETRIES)
	if err != nil {
		metrics.MARSendFailures.Inc()
		err = status.Errorf(codes.Internal, "Error while sending MAR with SID %s: %s", sid, err)
		glog.Error(err)
		return res, err
	}
	metrics.MARRequests.Inc()
	select {
	case resp, open := <-ch:
		if !open {
			metrics.SwxInvalidSessions.Inc()
			err = status.Errorf(codes.Aborted, "MAA for Session ID: %s is cancelled", sid)
			glog.Error(err)
			return res, err
		}
		maa, ok := resp.(*MAA)
		if !ok {
			metrics.SwxUnparseableMsg.Inc()
			err = status.Errorf(codes.Internal, "Invalid Response Type: %T, MAA expected.", resp)
			glog.Error(err)
			return res, err
		}
		err = diameter.TranslateDiamResultCode(maa.ResultCode)
		metrics.SwxResultCodes.WithLabelValues(strconv.FormatUint(uint64(maa.ResultCode), 10)).Inc()
		// If there is no base diameter error, check that there is no experimental error either
		if err == nil {
			err = diameter.TranslateDiamResultCode(maa.ExperimentalResult.ExperimentalResultCode)
			metrics.SwxExperimentalResultCodes.WithLabelValues(strconv.FormatUint(uint64(maa.ExperimentalResult.ExperimentalResultCode), 10)).Inc()
		}
		// According to spec 29.273, SIP-Auth-Data-Item(s) only present on SUCCESS
		if err != nil {
			return res, err
		}

		if shouldSendSar {
			profile, authorized, err := s.retrieveUserProfile(req.GetUserName())
			if err != nil {
				glog.Error(err)
			}
			// If user is unauthorized, don't send back auth vectors
			if !authorized {
				return res, err
			}
			res.UserProfile = profile
		}
		res.SipAuthVectors = getSIPAuthenticationVectors(maa.SIPAuthDataItems)
		// The only point when we cache vectors
		if s.cache != nil {
			res = s.cache.Put(res)
		}

	case <-time.After(time.Second * TIMEOUT_SECONDS):
		metrics.SwxTimeouts.Inc()
		err = status.Errorf(codes.DeadlineExceeded, "MAA Timed Out for Session ID: %s", sid)
		glog.Error(err)
	}
	return res, err
}

// retrieveUserProfile sends SARs with ServerAssignmentType AAA_USER_DATA_REQUEST, receives back SAA
// and returns the subscribers's Non-3GPP-User-Data profile
func (s *swxProxy) retrieveUserProfile(userName string) (*protos.AuthenticationAnswer_UserProfile, bool, error) {
	saa, err := s.sendSAR(userName, ServerAssignmentType_AAA_USER_DATA_REQUEST)
	if err != nil {
		return nil, true, err
	}
	if s.config.VerifyAuthorization && saa.UserData.Non3GPPIPAccess != datatype.Enumerated(Non3GPPIPAccess_ENABLED) {
		metrics.UnauthorizedAuthAttempts.Inc()
		return nil, false, status.Errorf(codes.PermissionDenied, "User %s is not authorized for Non-3GPP Subscription Access", userName)
	}
	if saa.UserData.SubscriptionId.SubscriptionIdType != END_USER_E164 {
		return nil, true, status.Error(
			codes.Internal,
			"Subscription ID type is not END_USER_E164; Cannot retrieve MSISDN",
		)
	}
	userProfile := &protos.AuthenticationAnswer_UserProfile{
		Msisdn: string(saa.UserData.SubscriptionId.SubscriptionIdData),
	}
	return userProfile, true, nil
}

// createMAR creates a Multimedia Authentication Request diameter msg with provided SessionID (sid)
// to be sent to HSS
func (s *swxProxy) createMAR(sid string, req *protos.AuthenticationRequest) (*diam.Message, error) {
	authScheme, err := convertAuthSchemeToString(req.GetAuthenticationScheme())
	if err != nil {
		return nil, err
	}

	msg := diameter.NewProxiableRequest(diam.MultimediaAuthentication, diam.TGPP_SWX_APP_ID, dict.Default)
	msg.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sid))
	msg.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(diam.TGPP_SWX_APP_ID)),
			diam.NewAVP(avp.VendorID, avp.Mbit, 0, datatype.Unsigned32(diameter.Vendor3GPP)),
		},
	})
	msg.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity(s.config.ClientCfg.Host))
	msg.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity(s.config.ClientCfg.Realm))
	msg.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(req.GetUserName()))
	msg.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Enumerated(1))
	msg.NewAVP(avp.SIPNumberAuthItems, avp.Mbit|avp.Vbit, uint32(diameter.Vendor3GPP), datatype.Unsigned32(req.GetSipNumAuthVectors()))
	msg.NewAVP(avp.RATType, avp.Mbit|avp.Vbit, diameter.Vendor3GPP, datatype.Enumerated(RadioAccessTechnologyType_WLAN))
	authDataAvp := []*diam.AVP{
		diam.NewAVP(avp.SIPAuthenticationScheme, avp.Mbit|avp.Vbit, uint32(diameter.Vendor3GPP), datatype.UTF8String(authScheme)),
	}
	if len(req.GetResyncInfo()) > 0 {
		authDataAvp = append(
			authDataAvp,
			diam.NewAVP(avp.SIPAuthorization, avp.Mbit|avp.Vbit, uint32(diameter.Vendor3GPP), datatype.OctetString(req.GetResyncInfo())),
		)
	}
	msg.NewAVP(avp.SIPAuthDataItem, avp.Mbit|avp.Vbit, diameter.Vendor3GPP, &diam.GroupedAVP{AVP: authDataAvp})
	return msg, nil
}

func handleMAA(s *swxProxy) diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		var maa MAA
		err := m.Unmarshal(&maa)
		if err != nil {
			metrics.SwxUnparseableMsg.Inc()
			glog.Errorf("MAA Unmarshal failed for remote %s & message %s: %s", c.RemoteAddr(), m, err)
			return
		}
		ch := s.requestTracker.DeregisterRequest(maa.SessionID)
		if ch != nil {
			ch <- &maa
		} else {
			metrics.SwxInvalidSessions.Inc()
			glog.Errorf("MAA SessionID %s not found. Message: %s, Remote: %s", maa.SessionID, m, c.RemoteAddr())
		}
	}
}

func getSIPAuthenticationVectors(items []SIPAuthDataItem) []*protos.AuthenticationAnswer_SIPAuthVector {
	var authVectors []*protos.AuthenticationAnswer_SIPAuthVector
	for _, item := range items {
		// If the auth scheme is unrecognized, don't include the vector
		authScheme, err := convertStringToAuthScheme(item.AuthScheme)
		if err != nil {
			glog.Error(err)
			continue
		}
		authVectors = append(
			authVectors,
			&protos.AuthenticationAnswer_SIPAuthVector{
				AuthenticationScheme: protos.AuthenticationScheme(authScheme),
				RandAutn:             item.Authenticate.Serialize(),
				Xres:                 item.Authorization.Serialize(),
				ConfidentialityKey:   item.ConfidentialityKey.Serialize(),
				IntegrityKey:         item.IntegrityKey.Serialize()})
	}
	return authVectors
}

func validateAuthRequest(req *protos.AuthenticationRequest) error {
	if req == nil {
		return fmt.Errorf("Nil authentication request provided")
	}
	if len(req.GetUserName()) == 0 {
		return fmt.Errorf("Empty user-name provided in authentication request")
	}
	if req.SipNumAuthVectors == 0 {
		return fmt.Errorf("SIPNumAuthVectors in authentication request must be greater than 0")
	}
	// imsi cannot be greater than 15 digits according to 3GPP Spec 23.003
	if len(req.GetUserName()) > 15 {
		return fmt.Errorf("Provided username %s is greater than 15 digits", req.GetUserName())
	}
	return nil
}

func convertStringToAuthScheme(maaScheme string) (protos.AuthenticationScheme, error) {
	switch maaScheme {
	case SipAuthScheme_EAP_AKA:
		return protos.AuthenticationScheme_EAP_AKA, nil
	case SipAuthScheme_EAP_AKA_PRIME:
		return protos.AuthenticationScheme_EAP_AKA_PRIME, nil
	default:
		return protos.AuthenticationScheme_EAP_AKA, fmt.Errorf("Unrecognized Authentication Scheme returned: %s", maaScheme)
	}
}

func convertAuthSchemeToString(scheme protos.AuthenticationScheme) (string, error) {
	switch scheme {
	case protos.AuthenticationScheme_EAP_AKA:
		return SipAuthScheme_EAP_AKA, nil
	case protos.AuthenticationScheme_EAP_AKA_PRIME:
		return SipAuthScheme_EAP_AKA_PRIME, nil
	default:
		return "", fmt.Errorf("Unrecognized Authentication Scheme returned: %v", scheme)
	}
}
