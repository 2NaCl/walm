package tenant

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TenantInfoList struct {
	Items []*TenantInfo `json:"items" description:"tenant list"`
}

//Tenant Info
type TenantInfo struct {
	TenantName            string                  `json:"tenantName" description:"name of the tenant"`
	TenantCreationTime    v1.Time                 `json:"tenantCreationTime" description:"create time of the tenant"`
	TenantLabels          map[string]string       `json:"tenantLabels"  description:"labels of the tenant"`
	TenantAnnotitions     map[string]string       `json:"tenantAnnotations"  description:"annotations of the tenant"`
	TenantStatus          string                  `json:"tenantStatus" description:"status of the tenant"`
	TenantQuotas          []*TenantQuota          `json:"tenantQuotas" description:"quotas of the tenant"`
	MultiTenant           bool                    `json:"multiTenant" description:"multi tenant"`
	Ready                 bool                    `json:"ready" description:"tenant ready status"`
	UnifyUnitTenantQuotas []*UnifyUnitTenantQuota `json:"unifyUnitTenantQuotas" description:"quotas of the tenant with unified unit"`
}

type UnifyUnitTenantQuota struct {
	QuotaName string                    `json:"quotaName" description:"quota name"`
	Hard      *UnifyUnitTenantQuotaInfo `json:"hard" description:"quota hard limit"`
	Used      *UnifyUnitTenantQuotaInfo `json:"used" description:"quota used"`
}

//Tenant Params Info
type TenantParams struct {
	TenantAnnotations map[string]string    `json:"tenantAnnotations"  description:"annotations of the tenant"`
	TenantLabels      map[string]string    `json:"tenantLabels"  description:"labels of the tenant"`
	TenantQuotas      []*TenantQuotaParams `json:"tenantQuotas" description:"quotas of the tenant"`
}

type TenantQuotaParams struct {
	QuotaName string           `json:"quotaName" description:"quota name"`
	Hard      *TenantQuotaInfo `json:"hard" description:"quota hard limit"`
}

type TenantQuota struct {
	QuotaName string           `json:"quotaName" description:"quota name"`
	Hard      *TenantQuotaInfo `json:"hard" description:"quota hard limit"`
	Used      *TenantQuotaInfo `json:"used" description:"quota used"`
}

//Quota Info
type TenantQuotaInfo struct {
	LimitCpu        string `json:"limitCpu"  description:"requests of the CPU"`
	LimitMemory     string `json:"limitMemory"  description:"limit of the memory"`
	RequestsCPU     string `json:"requestsCpu"  description:"requests of the CPU"`
	RequestsMemory  string `json:"requestsMemory"  description:"requests of the memory"`
	RequestsStorage string `json:"requestsStorage"  description:"requests of the storage"`
	Pods            string `json:"pods" description:"num of the pods"`
}

type UnifyUnitTenantQuotaInfo struct {
	LimitCpu        float64 `json:"limitCpu"  description:"requests of the CPU"`
	LimitMemory     int64   `json:"limitMemory"  description:"limit of the memory"`
	RequestsCPU     float64 `json:"requestsCpu"  description:"requests of the CPU"`
	RequestsMemory  int64   `json:"requestsMemory"  description:"requests of the memory"`
	RequestsStorage int64   `json:"requestsStorage"  description:"requests of the storage"`
	Pods            int64   `json:"pods" description:"num of the pods"`
}

/*
//Pod event Info
type PodEventInfo struct {
	FirstTimestamp time.Time `json:"first_timestamp" description:"first_timestamp of event"`
	LastTimestamp  time.Time `json:"last_timestamp" description:"last_timestamp of event"`
	Count          int       `json:"count" description:"count of event"`
	Type           string    `json:"type" description:"type of event"`
	Reason         string    `json:"reason" description:"reason of event"`
	Message        string    `json:"message" description:"message of event"`
}

//Pod log Info
type PodLogInfo struct {
	ContainerName string `json:"container_name" description:"name of container"`
	Log           string `json:"log" description:"log info"`
}

//Pod's events and log Info
type PodDetailInfo struct {
	Events []PodEventInfo `json:"events" description:"events info"`
	Log    []PodLogInfo   `json:"log" description:"logs info"`
}

//Service List for Tenant Context
type ServiceForTenantInfo struct {
	ApplicationType string `json:"application_type" description:"application_type of service"`
	ServiceStatus   string `json:"service_status" description:"service_status of service"`
	ServiceName     string `json:"service_name" description:"service_name of service"`
	ServiceHostname string `json:"service_hostname" description:"service_hostname of service"`
	Path            string `json:"proxy" description:"path of service"`
	Port            int    `json:"port" description:"port of service"`
}
*/
