package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"code.cloudfoundry.org/korifi/api/apierrors"
	"code.cloudfoundry.org/korifi/api/authorization"
	korifiv1alpha1 "code.cloudfoundry.org/korifi/controllers/api/v1alpha1"
	"code.cloudfoundry.org/korifi/controllers/controllers/workloads"
	"code.cloudfoundry.org/korifi/controllers/controllers/workloads/env"
	"code.cloudfoundry.org/korifi/controllers/webhooks"
	"code.cloudfoundry.org/korifi/tools/k8s"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	StartedState DesiredState = "STARTED"
	StoppedState DesiredState = "STOPPED"

	Kind               string = "CFApp"
	APIVersion         string = "korifi.cloudfoundry.org/v1alpha1"
	TimestampFormat    string = time.RFC3339
	CFAppGUIDLabel     string = "korifi.cloudfoundry.org/app-guid"
	AppResourceType    string = "App"
	AppEnvResourceType string = "App Env"
)

type AppRepo struct {
	namespaceRetriever   NamespaceRetriever
	userClientFactory    authorization.UserK8sClientFactory
	namespacePermissions *authorization.NamespacePermissions
	appConditionAwaiter  ConditionAwaiter[*korifiv1alpha1.CFApp]
}

func NewAppRepo(
	namespaceRetriever NamespaceRetriever,
	userClientFactory authorization.UserK8sClientFactory,
	authPerms *authorization.NamespacePermissions,
	appConditionAwaiter ConditionAwaiter[*korifiv1alpha1.CFApp],
) *AppRepo {
	return &AppRepo{
		namespaceRetriever:   namespaceRetriever,
		userClientFactory:    userClientFactory,
		namespacePermissions: authPerms,
		appConditionAwaiter:  appConditionAwaiter,
	}
}

type AppRecord struct {
	Name                  string
	GUID                  string
	EtcdUID               types.UID
	Revision              string
	SpaceGUID             string
	DropletGUID           string
	Labels                map[string]string
	Annotations           map[string]string
	State                 DesiredState
	Lifecycle             Lifecycle
	CreatedAt             string
	UpdatedAt             string
	IsStaged              bool
	envSecretName         string
	vcapServiceSecretName string
}

type DesiredState string

type Lifecycle struct {
	Type string
	Data LifecycleData
}

type LifecycleData struct {
	Buildpacks []string
	Stack      string
}

type AppEnvVarsRecord struct {
	Name                 string
	AppGUID              string
	SpaceGUID            string
	EnvironmentVariables map[string]string
}

type AppEnvRecord struct {
	AppGUID              string
	SpaceGUID            string
	EnvironmentVariables map[string]string
	SystemEnv            map[string]interface{}
}

type CurrentDropletRecord struct {
	AppGUID     string
	DropletGUID string
}

type CreateAppMessage struct {
	Name                 string
	SpaceGUID            string
	Labels               map[string]string
	Annotations          map[string]string
	State                DesiredState
	Lifecycle            Lifecycle
	EnvironmentVariables map[string]string
}

type PatchAppMessage struct {
	Name                 string
	AppGUID              string
	SpaceGUID            string
	Labels               map[string]string
	Annotations          map[string]string
	State                DesiredState
	Lifecycle            Lifecycle
	EnvironmentVariables map[string]string
}

type DeleteAppMessage struct {
	AppGUID   string
	SpaceGUID string
}

type CreateOrPatchAppEnvVarsMessage struct {
	AppGUID              string
	AppEtcdUID           types.UID
	SpaceGUID            string
	EnvironmentVariables map[string]string
}

type PatchAppEnvVarsMessage struct {
	AppGUID              string
	SpaceGUID            string
	EnvironmentVariables map[string]*string
}

type PatchAppMetadataMessage struct {
	MetadataPatch
	AppGUID   string
	SpaceGUID string
}

type SetCurrentDropletMessage struct {
	AppGUID     string
	DropletGUID string
	SpaceGUID   string
}

type SetAppDesiredStateMessage struct {
	AppGUID      string
	SpaceGUID    string
	DesiredState string
}

type ListAppsMessage struct {
	Names      []string
	Guids      []string
	SpaceGuids []string
}

type byName []AppRecord

func (a byName) Len() int {
	return len(a)
}

func (a byName) Less(i, j int) bool {
	return a[i].Name < a[j].Name
}

func (a byName) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (f *AppRepo) GetApp(ctx context.Context, authInfo authorization.Info, appGUID string) (AppRecord, error) {
	ns, err := f.namespaceRetriever.NamespaceFor(ctx, appGUID, AppResourceType)
	if err != nil {
		return AppRecord{}, err
	}

	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppRecord{}, fmt.Errorf("get-app failed to build user client: %w", err)
	}

	app := korifiv1alpha1.CFApp{}
	err = userClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: appGUID}, &app)
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to get app: %w", apierrors.FromK8sError(err, AppResourceType))
	}

	return cfAppToAppRecord(app), nil
}

func (f *AppRepo) GetAppByNameAndSpace(ctx context.Context, authInfo authorization.Info, appName string, spaceGUID string) (AppRecord, error) {
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppRecord{}, fmt.Errorf("get-app failed to build user client: %w", err)
	}

	appList := new(korifiv1alpha1.CFAppList)
	err = userClient.List(ctx, appList, client.InNamespace(spaceGUID))
	if err != nil {
		return AppRecord{}, apierrors.FromK8sError(fmt.Errorf("get app: failed to list apps: %w", err), SpaceResourceType)
	}

	var matchingApps []korifiv1alpha1.CFApp
	for _, app := range appList.Items {
		if app.Spec.DisplayName == appName {
			matchingApps = append(matchingApps, app)
		}
	}

	if len(matchingApps) == 0 {
		return AppRecord{}, apierrors.NewNotFoundError(fmt.Errorf("app %q in space %q not found", appName, spaceGUID), AppResourceType)
	}
	if len(matchingApps) > 1 {
		return AppRecord{}, fmt.Errorf("duplicate instances of app %q in space %q", appName, spaceGUID)
	}

	return cfAppToAppRecord(matchingApps[0]), nil
}

func (f *AppRepo) CreateApp(ctx context.Context, authInfo authorization.Info, appCreateMessage CreateAppMessage) (AppRecord, error) {
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	cfApp := appCreateMessage.toCFApp()
	err = userClient.Create(ctx, &cfApp)
	if err != nil {
		if validationError, ok := webhooks.WebhookErrorToValidationError(err); ok {
			if validationError.Type == webhooks.DuplicateNameErrorType {
				return AppRecord{}, apierrors.NewUniquenessError(err, validationError.GetMessage())
			}
		}

		return AppRecord{}, apierrors.FromK8sError(err, AppResourceType)
	}

	_, err = f.CreateOrPatchAppEnvVars(ctx, authInfo, CreateOrPatchAppEnvVarsMessage{
		AppGUID:              cfApp.Name,
		AppEtcdUID:           cfApp.UID,
		SpaceGUID:            cfApp.Namespace,
		EnvironmentVariables: appCreateMessage.EnvironmentVariables,
	})
	if err != nil {
		return AppRecord{}, err
	}

	return cfAppToAppRecord(cfApp), nil
}

func (f *AppRepo) PatchApp(ctx context.Context, authInfo authorization.Info, appPatchMessage PatchAppMessage) (AppRecord, error) {
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	app := korifiv1alpha1.CFApp{}
	err = userClient.Get(ctx, client.ObjectKey{Namespace: appPatchMessage.SpaceGUID, Name: appPatchMessage.AppGUID}, &app)
	if err != nil {
		return AppRecord{}, apierrors.FromK8sError(err, AppResourceType)
	}

	cfApp := appPatchMessage.toCFApp()
	err = k8s.PatchResource(ctx, userClient, &app, func() {
		app.Spec.Lifecycle = cfApp.Spec.Lifecycle
	})
	if err != nil {
		return AppRecord{}, apierrors.FromK8sError(err, AppResourceType)
	}

	_, err = f.CreateOrPatchAppEnvVars(ctx, authInfo, CreateOrPatchAppEnvVarsMessage{
		AppGUID:              app.Name,
		AppEtcdUID:           app.UID,
		SpaceGUID:            app.Namespace,
		EnvironmentVariables: appPatchMessage.EnvironmentVariables,
	})
	if err != nil {
		return AppRecord{}, err
	}

	return cfAppToAppRecord(app), nil
}

func (f *AppRepo) ListApps(ctx context.Context, authInfo authorization.Info, message ListAppsMessage) ([]AppRecord, error) {
	nsList, err := f.namespacePermissions.GetAuthorizedSpaceNamespaces(ctx, authInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces for spaces with user role bindings: %w", err)
	}

	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return []AppRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	var filteredApps []korifiv1alpha1.CFApp
	for ns := range nsList {
		appList := &korifiv1alpha1.CFAppList{}
		err := userClient.List(ctx, appList, client.InNamespace(ns))
		if k8serrors.IsForbidden(err) {
			continue
		}
		if err != nil {
			return []AppRecord{}, fmt.Errorf("failed to list apps in namespace %s: %w", ns, apierrors.FromK8sError(err, AppResourceType))
		}
		filteredApps = append(filteredApps, applyAppListFilter(appList.Items, message)...)
	}

	appRecords := returnAppList(filteredApps)

	// By default sort it by App.DisplayName
	sort.Sort(byName(appRecords))

	return appRecords, nil
}

func applyAppListFilter(appList []korifiv1alpha1.CFApp, message ListAppsMessage) []korifiv1alpha1.CFApp {
	nameFilterSpecified := len(message.Names) > 0
	guidsFilterSpecified := len(message.Guids) > 0
	spaceGUIDFilterSpecified := len(message.SpaceGuids) > 0

	var filtered []korifiv1alpha1.CFApp

	if guidsFilterSpecified {
		for _, app := range appList {
			for _, guid := range message.Guids {
				if appMatchesGUID(app, guid) {
					filtered = append(filtered, app)
				}
			}
		}
	}

	if guidsFilterSpecified && len(filtered) == 0 {
		return filtered
	}

	if len(filtered) > 0 {
		appList = filtered
		filtered = []korifiv1alpha1.CFApp{}
	}

	if !nameFilterSpecified && !spaceGUIDFilterSpecified {
		return appList
	}

	for _, app := range appList {
		if nameFilterSpecified && spaceGUIDFilterSpecified {
			for _, name := range message.Names {
				for _, spaceGUID := range message.SpaceGuids {
					if appBelongsToSpace(app, spaceGUID) && appMatchesName(app, name) {
						filtered = append(filtered, app)
					}
				}
			}
		} else if nameFilterSpecified {
			for _, name := range message.Names {
				if appMatchesName(app, name) {
					filtered = append(filtered, app)
				}
			}
		} else if spaceGUIDFilterSpecified {
			for _, spaceGUID := range message.SpaceGuids {
				if appBelongsToSpace(app, spaceGUID) {
					filtered = append(filtered, app)
				}
			}
		}
	}

	return filtered
}

func appBelongsToSpace(app korifiv1alpha1.CFApp, spaceGUID string) bool {
	return app.Namespace == spaceGUID
}

func appMatchesName(app korifiv1alpha1.CFApp, name string) bool {
	return app.Spec.DisplayName == name
}

func appMatchesGUID(app korifiv1alpha1.CFApp, guid string) bool {
	return app.Name == guid
}

func returnAppList(appList []korifiv1alpha1.CFApp) []AppRecord {
	appRecords := make([]AppRecord, 0, len(appList))

	for _, app := range appList {
		appRecords = append(appRecords, cfAppToAppRecord(app))
	}
	return appRecords
}

func (f *AppRepo) PatchAppEnvVars(ctx context.Context, authInfo authorization.Info, message PatchAppEnvVarsMessage) (AppEnvVarsRecord, error) {
	secretObj := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenerateEnvSecretName(message.AppGUID),
			Namespace: message.SpaceGUID,
		},
	}

	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppEnvVarsRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	_, err = controllerutil.CreateOrPatch(ctx, userClient, &secretObj, func() error {
		secretObj.StringData = map[string]string{}
		for k, v := range message.EnvironmentVariables {
			if v == nil {
				delete(secretObj.Data, k)
			} else {
				secretObj.StringData[k] = *v
			}
		}
		return nil
	})
	if err != nil {
		return AppEnvVarsRecord{}, apierrors.FromK8sError(err, AppEnvResourceType)
	}

	return appEnvVarsSecretToRecord(secretObj), nil
}

func (f *AppRepo) CreateOrPatchAppEnvVars(ctx context.Context, authInfo authorization.Info, envVariables CreateOrPatchAppEnvVarsMessage) (AppEnvVarsRecord, error) {
	secretObj := appEnvVarsRecordToSecret(envVariables)

	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppEnvVarsRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	_, err = controllerutil.CreateOrPatch(ctx, userClient, &secretObj, func() error {
		secretObj.StringData = envVariables.EnvironmentVariables
		return nil
	})

	if err != nil {
		return AppEnvVarsRecord{}, apierrors.FromK8sError(err, AppEnvResourceType)
	}
	return appEnvVarsSecretToRecord(secretObj), nil
}

func (f *AppRepo) PatchAppMetadata(ctx context.Context, authInfo authorization.Info, message PatchAppMetadataMessage) (AppRecord, error) {
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	app := new(korifiv1alpha1.CFApp)
	err = userClient.Get(ctx, client.ObjectKey{Namespace: message.SpaceGUID, Name: message.AppGUID}, app)
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to get app: %w", apierrors.FromK8sError(err, AppResourceType))
	}

	err = patchMetadata(ctx, userClient, app, message.MetadataPatch, AppResourceType)
	if err != nil {
		return AppRecord{}, err
	}

	return cfAppToAppRecord(*app), nil
}

func (f *AppRepo) SetCurrentDroplet(ctx context.Context, authInfo authorization.Info, message SetCurrentDropletMessage) (CurrentDropletRecord, error) {
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return CurrentDropletRecord{}, fmt.Errorf("set-current-droplet: failed to create k8s user client: %w", err)
	}

	cfApp := &korifiv1alpha1.CFApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      message.AppGUID,
			Namespace: message.SpaceGUID,
		},
	}

	err = k8s.PatchResource(ctx, userClient, cfApp, func() {
		cfApp.Spec.CurrentDropletRef = corev1.LocalObjectReference{Name: message.DropletGUID}
	})
	if err != nil {
		return CurrentDropletRecord{}, fmt.Errorf("failed to set app droplet: %w", apierrors.FromK8sError(err, AppResourceType))
	}

	_, err = f.appConditionAwaiter.AwaitCondition(ctx, userClient, cfApp, workloads.StatusConditionStaged)
	if err != nil {
		return CurrentDropletRecord{}, fmt.Errorf("failed to await the app staged condition: %w", apierrors.FromK8sError(err, AppResourceType))
	}

	return CurrentDropletRecord{
		AppGUID:     message.AppGUID,
		DropletGUID: message.DropletGUID,
	}, nil
}

func (f *AppRepo) SetAppDesiredState(ctx context.Context, authInfo authorization.Info, message SetAppDesiredStateMessage) (AppRecord, error) {
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	cfApp := &korifiv1alpha1.CFApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      message.AppGUID,
			Namespace: message.SpaceGUID,
		},
	}

	err = k8s.PatchResource(ctx, userClient, cfApp, func() {
		cfApp.Spec.DesiredState = korifiv1alpha1.DesiredState(message.DesiredState)
	})
	if err != nil {
		return AppRecord{}, fmt.Errorf("failed to set app desired state: %w", apierrors.FromK8sError(err, AppResourceType))
	}

	return cfAppToAppRecord(*cfApp), nil
}

func (f *AppRepo) DeleteApp(ctx context.Context, authInfo authorization.Info, message DeleteAppMessage) error {
	cfApp := &korifiv1alpha1.CFApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      message.AppGUID,
			Namespace: message.SpaceGUID,
		},
	}
	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return fmt.Errorf("failed to build user client: %w", err)
	}

	return apierrors.FromK8sError(userClient.Delete(ctx, cfApp), AppResourceType)
}

func (f *AppRepo) GetAppEnv(ctx context.Context, authInfo authorization.Info, appGUID string) (AppEnvRecord, error) {
	app, err := f.GetApp(ctx, authInfo, appGUID)
	if err != nil {
		return AppEnvRecord{}, err
	}

	userClient, err := f.userClientFactory.BuildClient(authInfo)
	if err != nil {
		return AppEnvRecord{}, fmt.Errorf("failed to build user client: %w", err)
	}

	appEnvVarMap := map[string]string{}
	if app.envSecretName != "" {
		appEnvVarSecret := new(corev1.Secret)
		err = userClient.Get(ctx, types.NamespacedName{Name: app.envSecretName, Namespace: app.SpaceGUID}, appEnvVarSecret)
		if err != nil {
			return AppEnvRecord{}, fmt.Errorf("error finding environment variable Secret %q for App %q: %w",
				app.envSecretName,
				app.GUID,
				apierrors.FromK8sError(err, AppEnvResourceType))
		}
		appEnvVarMap = convertByteSliceValuesToStrings(appEnvVarSecret.Data)
	}

	systemEnvMap := map[string]interface{}{}
	if app.vcapServiceSecretName != "" {
		vcapServiceSecret := new(corev1.Secret)
		err = userClient.Get(ctx, types.NamespacedName{Name: app.vcapServiceSecretName, Namespace: app.SpaceGUID}, vcapServiceSecret)
		if err != nil {
			return AppEnvRecord{}, fmt.Errorf("error finding VCAP Service Secret %q for App %q: %w",
				app.vcapServiceSecretName,
				app.GUID,
				apierrors.FromK8sError(err, AppEnvResourceType))
		}

		if vcapServicesData, ok := vcapServiceSecret.Data["VCAP_SERVICES"]; ok {
			vcapServicesPresenter := new(env.VcapServicesPresenter)
			if err = json.Unmarshal(vcapServicesData, &vcapServicesPresenter); err != nil {
				return AppEnvRecord{}, fmt.Errorf("error unmarshalling VCAP Service Secret %q for App %q: %w",
					app.vcapServiceSecretName,
					app.GUID,
					apierrors.FromK8sError(err, AppEnvResourceType))
			}

			if len(vcapServicesPresenter.UserProvided) > 0 {
				systemEnvMap["VCAP_SERVICES"] = vcapServicesPresenter
			}
		}
	}

	appEnvRecord := AppEnvRecord{
		AppGUID:              appGUID,
		SpaceGUID:            app.SpaceGUID,
		EnvironmentVariables: appEnvVarMap,
		SystemEnv:            systemEnvMap,
	}

	return appEnvRecord, nil
}

func GenerateEnvSecretName(appGUID string) string {
	return appGUID + "-env"
}

func (m *CreateAppMessage) toCFApp() korifiv1alpha1.CFApp {
	guid := uuid.NewString()
	return korifiv1alpha1.CFApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:        guid,
			Namespace:   m.SpaceGUID,
			Labels:      m.Labels,
			Annotations: m.Annotations,
		},
		Spec: korifiv1alpha1.CFAppSpec{
			DisplayName:   m.Name,
			DesiredState:  korifiv1alpha1.DesiredState(m.State),
			EnvSecretName: GenerateEnvSecretName(guid),
			Lifecycle: korifiv1alpha1.Lifecycle{
				Type: korifiv1alpha1.LifecycleType(m.Lifecycle.Type),
				Data: korifiv1alpha1.LifecycleData{
					Buildpacks: m.Lifecycle.Data.Buildpacks,
					Stack:      m.Lifecycle.Data.Stack,
				},
			},
		},
	}
}

func (m *PatchAppMessage) toCFApp() korifiv1alpha1.CFApp {
	return korifiv1alpha1.CFApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:        m.AppGUID,
			Namespace:   m.SpaceGUID,
			Labels:      m.Labels,
			Annotations: m.Annotations,
		},
		Spec: korifiv1alpha1.CFAppSpec{
			DisplayName:  m.Name,
			DesiredState: korifiv1alpha1.DesiredState(m.State),
			Lifecycle: korifiv1alpha1.Lifecycle{
				Type: korifiv1alpha1.LifecycleType(m.Lifecycle.Type),
				Data: korifiv1alpha1.LifecycleData{
					Buildpacks: m.Lifecycle.Data.Buildpacks,
					Stack:      m.Lifecycle.Data.Stack,
				},
			},
		},
	}
}

func cfAppToAppRecord(cfApp korifiv1alpha1.CFApp) AppRecord {
	updatedAtTime, _ := getTimeLastUpdatedTimestamp(&cfApp.ObjectMeta)

	return AppRecord{
		GUID:        cfApp.Name,
		EtcdUID:     cfApp.GetUID(),
		Revision:    getLabelOrAnnotation(cfApp.GetAnnotations(), korifiv1alpha1.CFAppRevisionKey),
		Name:        cfApp.Spec.DisplayName,
		SpaceGUID:   cfApp.Namespace,
		DropletGUID: cfApp.Spec.CurrentDropletRef.Name,
		Labels:      cfApp.Labels,
		Annotations: cfApp.Annotations,
		State:       DesiredState(cfApp.Spec.DesiredState),
		Lifecycle: Lifecycle{
			Type: string(cfApp.Spec.Lifecycle.Type),
			Data: LifecycleData{
				Buildpacks: cfApp.Spec.Lifecycle.Data.Buildpacks,
				Stack:      cfApp.Spec.Lifecycle.Data.Stack,
			},
		},
		CreatedAt:             cfApp.CreationTimestamp.UTC().Format(TimestampFormat),
		UpdatedAt:             updatedAtTime,
		IsStaged:              meta.IsStatusConditionTrue(cfApp.Status.Conditions, workloads.StatusConditionStaged),
		envSecretName:         cfApp.Spec.EnvSecretName,
		vcapServiceSecretName: cfApp.Status.VCAPServicesSecretName,
	}
}

func appEnvVarsRecordToSecret(envVars CreateOrPatchAppEnvVarsMessage) corev1.Secret {
	labels := make(map[string]string, 1)
	labels[CFAppGUIDLabel] = envVars.AppGUID
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenerateEnvSecretName(envVars.AppGUID),
			Namespace: envVars.SpaceGUID,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: APIVersion,
					Kind:       Kind,
					Name:       envVars.AppGUID,
					UID:        envVars.AppEtcdUID,
				},
			},
		},
		StringData: envVars.EnvironmentVariables,
	}
}

func appEnvVarsSecretToRecord(envVars corev1.Secret) AppEnvVarsRecord {
	appGUID := strings.TrimSuffix(envVars.Name, "-env")
	return AppEnvVarsRecord{
		Name:                 envVars.Name,
		AppGUID:              appGUID,
		SpaceGUID:            envVars.Namespace,
		EnvironmentVariables: convertByteSliceValuesToStrings(envVars.Data),
	}
}

func convertByteSliceValuesToStrings(inputMap map[string][]byte) map[string]string {
	// StringData is a write-only field of a corev1.Secret, the real data lives in .Data and is []byte & base64 encoded
	outputMap := make(map[string]string, len(inputMap))
	for k, v := range inputMap {
		outputMap[k] = string(v)
	}
	return outputMap
}
