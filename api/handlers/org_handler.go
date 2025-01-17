package handlers

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	ctrl "sigs.k8s.io/controller-runtime"

	"code.cloudfoundry.org/korifi/api/apierrors"
	"code.cloudfoundry.org/korifi/api/authorization"
	"code.cloudfoundry.org/korifi/api/payloads"
	"code.cloudfoundry.org/korifi/api/presenter"
	"code.cloudfoundry.org/korifi/api/repositories"
)

const (
	OrgsPath       = "/v3/organizations"
	OrgPath        = "/v3/organizations/{guid}"
	OrgDomainsPath = "/v3/organizations/{guid}/domains"
)

//counterfeiter:generate -o fake -fake-name OrgRepository . CFOrgRepository
type CFOrgRepository interface {
	CreateOrg(context.Context, authorization.Info, repositories.CreateOrgMessage) (repositories.OrgRecord, error)
	ListOrgs(context.Context, authorization.Info, repositories.ListOrgsMessage) ([]repositories.OrgRecord, error)
	DeleteOrg(context.Context, authorization.Info, repositories.DeleteOrgMessage) error
	GetOrg(context.Context, authorization.Info, string) (repositories.OrgRecord, error)
	PatchOrgMetadata(context.Context, authorization.Info, repositories.PatchOrgMetadataMessage) (repositories.OrgRecord, error)
}

type OrgHandler struct {
	handlerWrapper                           *AuthAwareHandlerFuncWrapper
	apiBaseURL                               url.URL
	orgRepo                                  CFOrgRepository
	domainRepo                               CFDomainRepository
	decoderValidator                         *DecoderValidator
	userCertificateExpirationWarningDuration time.Duration
}

func NewOrgHandler(apiBaseURL url.URL, orgRepo CFOrgRepository, domainRepo CFDomainRepository, decoderValidator *DecoderValidator, userCertificateExpirationWarningDuration time.Duration) *OrgHandler {
	return &OrgHandler{
		handlerWrapper:                           NewAuthAwareHandlerFuncWrapper(ctrl.Log.WithName("Org Handler")),
		apiBaseURL:                               apiBaseURL,
		orgRepo:                                  orgRepo,
		domainRepo:                               domainRepo,
		decoderValidator:                         decoderValidator,
		userCertificateExpirationWarningDuration: userCertificateExpirationWarningDuration,
	}
}

func (h *OrgHandler) orgCreateHandler(ctx context.Context, logger logr.Logger, authInfo authorization.Info, r *http.Request) (*HandlerResponse, error) {
	var payload payloads.OrgCreate
	if err := h.decoderValidator.DecodeAndValidateJSONPayload(r, &payload); err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "invalid-payload-for-create-org")
	}

	org := payload.ToMessage()
	record, err := h.orgRepo.CreateOrg(ctx, authInfo, org)
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "Failed to create org", "Org Name", payload.Name)
	}

	return NewHandlerResponse(http.StatusCreated).WithBody(presenter.ForOrg(record, h.apiBaseURL)), nil
}

func (h *OrgHandler) orgPatchHandler(ctx context.Context, logger logr.Logger, authInfo authorization.Info, r *http.Request) (*HandlerResponse, error) {
	vars := mux.Vars(r)
	orgGUID := vars["guid"]

	_, err := h.orgRepo.GetOrg(ctx, authInfo, orgGUID)
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, apierrors.ForbiddenAsNotFound(err), "Failed to fetch org from Kubernetes", "OrgGUID", orgGUID)
	}

	var payload payloads.OrgPatch
	if err = h.decoderValidator.DecodeAndValidateJSONPayload(r, &payload); err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "failed to decode payload")
	}

	org, err := h.orgRepo.PatchOrgMetadata(ctx, authInfo, payload.ToMessage(orgGUID))
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "Failed to patch org metadata", "OrgGUID", orgGUID)
	}

	return NewHandlerResponse(http.StatusOK).WithBody(presenter.ForOrg(org, h.apiBaseURL)), nil
}

func (h *OrgHandler) orgDeleteHandler(ctx context.Context, logger logr.Logger, authInfo authorization.Info, r *http.Request) (*HandlerResponse, error) {
	vars := mux.Vars(r)
	orgGUID := vars["guid"]

	deleteOrgMessage := repositories.DeleteOrgMessage{
		GUID: orgGUID,
	}
	err := h.orgRepo.DeleteOrg(ctx, authInfo, deleteOrgMessage)
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, apierrors.ForbiddenAsNotFound(err), "Failed to delete org", "OrgGUID", orgGUID)
	}

	return NewHandlerResponse(http.StatusAccepted).WithHeader("Location", presenter.JobURLForRedirects(orgGUID, presenter.OrgDeleteOperation, h.apiBaseURL)), nil
}

func (h *OrgHandler) orgListHandler(ctx context.Context, logger logr.Logger, authInfo authorization.Info, r *http.Request) (*HandlerResponse, error) {
	names := parseCommaSeparatedList(r.URL.Query().Get("names"))

	orgs, err := h.orgRepo.ListOrgs(ctx, authInfo, repositories.ListOrgsMessage{Names: names})
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "failed to fetch orgs")
	}

	resp := NewHandlerResponse(http.StatusOK).WithBody(presenter.ForOrgList(orgs, h.apiBaseURL, *r.URL))
	notAfter, certParsed := decodePEMNotAfter(authInfo.CertData)

	if !isExpirationValid(notAfter, h.userCertificateExpirationWarningDuration, certParsed) {
		certWarningMsg := "Warning: The client certificate you provided for user authentication expires at %s, which exceeds the recommended validity duration of %s. Ask your platform provider to issue you a short-lived certificate credential or to configure your authentication to generate short-lived credentials automatically."
		resp = resp.WithHeader("X-Cf-Warnings", fmt.Sprintf(certWarningMsg, notAfter.Format(time.RFC3339), h.userCertificateExpirationWarningDuration))
	}
	return resp, nil
}

func (h *OrgHandler) orgListDomainHandler(ctx context.Context, logger logr.Logger, authInfo authorization.Info, r *http.Request) (*HandlerResponse, error) {
	vars := mux.Vars(r)
	orgGUID := vars["guid"]

	if _, err := h.orgRepo.GetOrg(ctx, authInfo, orgGUID); err != nil {
		return nil, apierrors.LogAndReturn(logger, apierrors.ForbiddenAsNotFound(err), "Unable to get organization")
	}

	if err := r.ParseForm(); err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "Unable to parse request query parameters")
	}

	domainListFilter := new(payloads.DomainList)
	err := payloads.Decode(domainListFilter, r.Form)
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "Unable to decode request query parameters")
	}

	domainList, err := h.domainRepo.ListDomains(ctx, authInfo, domainListFilter.ToMessage())
	if err != nil {
		return nil, apierrors.LogAndReturn(logger, err, "Failed to fetch domain(s) from Kubernetes")
	}

	return NewHandlerResponse(http.StatusOK).WithBody(presenter.ForDomainList(domainList, h.apiBaseURL, *r.URL)), nil
}

func (h *OrgHandler) RegisterRoutes(router *mux.Router) {
	router.Path(OrgsPath).Methods("GET").HandlerFunc(h.handlerWrapper.Wrap(h.orgListHandler))
	router.Path(OrgsPath).Methods("POST").HandlerFunc(h.handlerWrapper.Wrap(h.orgCreateHandler))
	router.Path(OrgPath).Methods("DELETE").HandlerFunc(h.handlerWrapper.Wrap(h.orgDeleteHandler))
	router.Path(OrgPath).Methods("PATCH").HandlerFunc(h.handlerWrapper.Wrap(h.orgPatchHandler))
	router.Path(OrgDomainsPath).Methods("GET").HandlerFunc(h.handlerWrapper.Wrap(h.orgListDomainHandler))
}

func decodePEMNotAfter(certPEM []byte) (time.Time, bool) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return time.Now(), false
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return time.Now(), false
	}

	return cert.NotAfter, true
}

func isExpirationValid(notAfter time.Time, maxDuration time.Duration, certParsed bool) bool {
	return (certParsed && time.Until(notAfter) < maxDuration) || !certParsed
}
