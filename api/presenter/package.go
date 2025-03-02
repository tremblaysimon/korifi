package presenter

import (
	"net/url"

	"code.cloudfoundry.org/korifi/api/repositories"
)

const (
	packagesBase = "/v3/packages"
)

type PackageResponse struct {
	GUID          string        `json:"guid"`
	Type          string        `json:"type"`
	Data          PackageData   `json:"data"`
	State         string        `json:"state"`
	Relationships Relationships `json:"relationships"`
	Links         PackageLinks  `json:"links"`
	Metadata      Metadata      `json:"metadata"`
	CreatedAt     string        `json:"created_at"`
	UpdatedAt     string        `json:"updated_at"`
}

type PackageData struct{}

type PackageLinks struct {
	Self     Link `json:"self"`
	Upload   Link `json:"upload"`
	Download Link `json:"download"`
	App      Link `json:"app"`
}

func ForPackage(record repositories.PackageRecord, baseURL url.URL) PackageResponse {
	if record.Labels == nil {
		record.Labels = map[string]string{}
	}
	if record.Annotations == nil {
		record.Annotations = map[string]string{}
	}
	return PackageResponse{
		GUID:      record.GUID,
		Type:      record.Type,
		State:     record.State,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
		Relationships: Relationships{
			"app": Relationship{
				Data: &RelationshipData{
					GUID: record.AppGUID,
				},
			},
		},
		Links: PackageLinks{
			Self: Link{
				HRef: buildURL(baseURL).appendPath(packagesBase, record.GUID).build(),
			},
			Upload: Link{
				HRef:   buildURL(baseURL).appendPath(packagesBase, record.GUID, "upload").build(),
				Method: "POST",
			},
			Download: Link{
				HRef:   buildURL(baseURL).appendPath(packagesBase, record.GUID, "download").build(),
				Method: "GET",
			},
			App: Link{
				HRef: buildURL(baseURL).appendPath(appsBase, record.AppGUID).build(),
			},
		},
		Metadata: Metadata{
			Labels:      record.Labels,
			Annotations: record.Annotations,
		},
	}
}

func ForPackageList(packageRecordList []repositories.PackageRecord, baseURL, requestURL url.URL) ListResponse {
	packageResponses := make([]interface{}, 0, len(packageRecordList))
	for _, currentPackage := range packageRecordList {
		packageResponses = append(packageResponses, ForPackage(currentPackage, baseURL))
	}

	return ForList(packageResponses, baseURL, requestURL)
}
