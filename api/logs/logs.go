package logs

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	durationUtil "github.com/flanksource/commons/duration"
	"github.com/flanksource/kommons"
)

var GlobalBackends []SearchBackend

// SearchConfig refers to the main configuration
// that consists of configuration for a list of backends.
type SearchConfig struct {
	// Path is the path of this config file
	Path     string               `yaml:"-" json:"-"`
	Backends SearchBackendConfigs `yaml:"backends,omitempty" json:"backends,omitempty"`
}

// +kubebuilder:object:generate=true
type SearchBackendConfig struct {
	ElasticSearch *ElasticSearchBackendConfig    `json:"elasticsearch,omitempty" yaml:"elasticsearch,omitempty"`
	OpenSearch    *OpenSearchBackendConfig       `json:"opensearch,omitempty" yaml:"opensearch,omitempty"`
	CloudWatch    *CloudWatchBackendConfig       `json:"cloudwatch,omitempty" yaml:"cloudwatch,omitempty"`
	Kubernetes    *KubernetesSearchBackendConfig `json:"kubernetes,omitempty" yaml:"kubernetes,omitempty"`
	File          *FileSearchBackendConfig       `json:"file,omitempty" yaml:"file,omitempty"`
}

func NewSearchBackend(api SearchAPI) SearchBackend {
	return SearchBackend{
		API: api,
	}
}

type SearchBackend struct {
	API SearchAPI
}

type Routes []SearchRoute

func (t Routes) MatchRoute(q *SearchParams) (match bool, isAdditive bool) {
	for _, route := range t {
		if route.Match(q) {
			return true, route.IsAdditive
		}
	}

	return false, false
}

// +kubebuilder:object:generate=true
type CommonBackend struct {
	Routes Routes `yaml:"routes,omitempty" json:"routes,omitempty"`

	// Labels are custom labels specified in the configuration file for a backend
	// that will be attached to each log line returned by that backend.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type SearchBackendConfigs []SearchBackendConfig

// +kubebuilder:object:generate=true
type SearchRoute struct {
	Type       string            `yaml:"type,omitempty" json:"type,omitempty"`
	IdPrefix   string            `yaml:"idPrefix,omitempty" json:"id_prefix,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	IsAdditive bool              `yaml:"additive,omitempty" json:"is_additive,omitempty"`
}

func (t *SearchRoute) Match(q *SearchParams) bool {
	if t.Type != "" && t.Type != q.Type {
		return false
	}

	if t.IdPrefix != "" && !strings.HasPrefix(q.Id, t.IdPrefix) {
		return false
	}

	for k, v := range t.Labels {
		qVal, ok := q.Labels[k]
		if !ok {
			return false
		}

		configuredLabels := strings.Split(v, ",")
		if !collections.MatchItems(qVal, configuredLabels...) {
			return false
		}
	}

	return true
}

// +kubebuilder:object:generate=true
type KubernetesSearchBackendConfig struct {
	CommonBackend `json:",inline" yaml:",inline"`
	// empty kubeconfig indicates to use the current kubeconfig for connection
	Kubeconfig *kommons.EnvVar `json:"kubeconfig,omitempty"`
	//namespace to search the kommons.EnvVar in
	Namespace string `json:"namespace,omitempty"`
}

// +kubebuilder:object:generate=true
type FileSearchBackendConfig struct {
	CommonBackend `json:",inline" yaml:",inline"`
	Paths         []string `yaml:"path,omitempty" json:"path,omitempty"`
}

// +kubebuilder:object:generate=true
type AWSAuthentication struct {
	Region    string          `yaml:"region,omitempty" json:"region,omitempty"`
	AccessKey *kommons.EnvVar `yaml:"access_key,omitempty" json:"access_key,omitempty"`
	SecretKey *kommons.EnvVar `yaml:"secret_key,omitempty" json:"secret_key,omitempty"`
}

// +kubebuilder:object:generate=true
type CloudWatchBackendConfig struct {
	CommonBackend `json:",inline" yaml:",inline"`
	Auth          AWSAuthentication `yaml:"auth,omitempty" json:"auth,omitempty"`
	Namespace     string            `yaml:"namespace,omitempty" json:"namespace,omitempty"` // Namespace to search the kommons.EnvVar in
	LogGroup      string            `yaml:"log_group,omitempty" json:"log_group,omitempty"`
	Query         string            `yaml:"query,omitempty" json:"query,omitempty"`
}

// +kubebuilder:object:generate=true
// ElasticSearchFields defines the fields to use for the timestamp and message
// and excluding certain fields from the message
type ElasticSearchFields struct {
	Timestamp  string   `yaml:"timestamp,omitempty" json:"timestamp,omitempty"`   // Timestamp is the field used to extract the timestamp
	Message    string   `yaml:"message,omitempty" json:"message,omitempty"`       // Message is the field used to extract the message
	Exclusions []string `yaml:"exclusions,omitempty" json:"exclusions,omitempty"` // Exclusions are the fields that'll be extracted from the labels
}

// +kubebuilder:object:generate=true
type ElasticSearchBackendConfig struct {
	CommonBackend `json:",inline" yaml:",inline"`
	Address       string              `yaml:"address,omitempty" json:"address,omitempty"`
	Query         string              `yaml:"query,omitempty" json:"query,omitempty"`
	Index         string              `yaml:"index,omitempty" json:"index,omitempty"`
	Namespace     string              `json:"namespace,omitempty"` // Namespace to search the kommons.EnvVar in
	Fields        ElasticSearchFields `yaml:"fields,omitempty" json:"fields,omitempty"`

	CloudID  *kommons.EnvVar `yaml:"cloudID,omitempty" json:"cloud_id,omitempty"`
	APIKey   *kommons.EnvVar `yaml:"apiKey,omitempty" json:"api_key,omitempty"`
	Username *kommons.EnvVar `yaml:"username,omitempty" json:"username,omitempty"`
	Password *kommons.EnvVar `yaml:"password,omitempty" json:"password,omitempty"`
}

// +kubebuilder:object:generate=true
type OpenSearchBackendConfig struct {
	CommonBackend `json:",inline" yaml:",inline"`
	Address       string              `yaml:"address,omitempty" json:"address,omitempty"`
	Query         string              `yaml:"query,omitempty" json:"query,omitempty"`
	Index         string              `yaml:"index,omitempty" json:"index,omitempty"`
	Namespace     string              `yaml:"namespace,omitempty" json:"namespace,omitempty"` // Namespace to search the kommons.EnvVar in
	Fields        ElasticSearchFields `yaml:"fields,omitempty" json:"fields,omitempty"`

	Username *kommons.EnvVar `yaml:"username,omitempty" json:"username,omitempty"`
	Password *kommons.EnvVar `yaml:"password,omitempty" json:"password,omitempty"`
}

type SearchParams struct {
	// Limit is the maximum number of results to return.
	Limit      int64 `json:"limit,omitempty"`
	LimitBytes int64 `json:"limitBytes,omitempty"`
	// The page token, returned by a previous call, to request the next page of results.
	Page string `json:"page,omitempty"`
	// comma separated list of labels to filter the results. key1=value1,key2=value2
	Labels map[string]string `json:"labels,omitempty"`
	// A generic query string, that is rewritten to the underlying system,
	// If the underlying system does not support queries, than this query is applied on the returned results
	Query string `json:"query,omitempty"`
	// A RFC3339 timestamp or an age string (e.g. "1h", "2d", "1w"), default to 1h
	Start string `json:"start,omitempty"`
	// A RFC3339 timestamp or an age string (e.g. "1h", "2d", "1w")
	End string `json:"end,omitempty"`
	// The type of logs to find, e.g. KubernetesNode, KubernetesService, KubernetesPod, VM, etc. Type and ID are used to route search requests
	Type string `json:"type,omitempty"`
	// The identifier of the type of logs to find, e.g. k8s-node-1, k8s-service-1, k8s-pod-1, vm-1, etc.
	// The ID should include include any cluster/namespace/account information required for routing
	Id string `json:"id,omitempty"`
	// Limits the number of log messages return per item, e.g. pod
	LimitPerItem int64 `json:"limitPerItem,omitempty"`
	// Limits the number of bytes returned per item, e.g. pod
	LimitBytesPerItem int64 `json:"limitBytesPerItem,omitempty"`

	start *time.Time `json:"-"`
	end   *time.Time `json:"-"`
}

// SetDefaults sets the default values for the search params
// if they are not set
func (t *SearchParams) SetDefaults() {
	if t.Start == "" {
		t.Start = "1h"
	}

	if t.LimitPerItem == 0 {
		t.LimitPerItem = 100
	}

	if t.Limit <= 0 {
		t.Limit = 50
	}

	if t.LimitBytesPerItem == 0 {
		t.LimitBytesPerItem = 100 * 1024
	}
}

func (p SearchParams) GetStartISO() string {
	start := p.GetStart()
	if start == nil {
		return ""
	}

	return start.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (p *SearchParams) GetStart() *time.Time {
	if p.start != nil {
		return p.start
	}

	if duration, err := durationUtil.ParseDuration(p.Start); err == nil {
		t := time.Now().Add(-time.Duration(duration))
		p.start = &t
	} else if t, err := time.Parse(time.RFC3339, p.Start); err == nil {
		p.start = &t
	}

	return p.start
}

func (p *SearchParams) GetEnd() *time.Time {
	if p.end != nil {
		return p.end
	}

	if duration, err := durationUtil.ParseDuration(p.End); err == nil {
		t := time.Now().Add(-time.Duration(duration))
		p.end = &t
	} else if t, err := time.Parse(time.RFC3339, p.End); err == nil {
		p.end = &t
	}

	return p.end
}

func (q SearchParams) String() string {
	s := ""
	if q.Type != "" {
		s += fmt.Sprintf("type=%s ", q.Type)
	}
	if q.Id != "" {
		s += fmt.Sprintf("id=%s ", q.Id)
	}
	if q.Start != "" {
		s += fmt.Sprintf("start=%s ", q.Start)
	}
	if q.Query != "" {
		s += fmt.Sprintf("query=%s ", q.Query)
	}
	if len(q.Labels) > 0 {
		s += fmt.Sprintf("labels=%v ", q.Labels)
	}
	if q.End != "" {
		s += fmt.Sprintf("end=%s ", q.End)
	}
	if q.Limit > 0 {
		s += fmt.Sprintf("limit=%d ", q.Limit)
	}
	if q.Page != "" {
		s += fmt.Sprintf("page=%s ", q.Page)
	}
	return s
}

type SearchResults struct {
	Total    int      `json:"total,omitempty"`
	Results  []Result `json:"results,omitempty"`
	NextPage string   `json:"nextPage,omitempty"`
}

func (r *SearchResults) Append(other *SearchResults) {
	r.Results = append(r.Results, other.Results...)
	r.Total += other.Total
	r.NextPage = other.NextPage
}

type Result struct {
	// Id is the unique identifier provided by the underlying system, use to link to a point in time of a log stream
	Id string `json:"id,omitempty"`
	// RFC3339 timestamp
	Time    string            `json:"timestamp,omitempty"`
	Message string            `json:"message,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

func (r Result) Process() Result {
	scanner := bufio.NewScanner(strings.NewReader(r.Message))
	scanner.Split(bufio.ScanWords)
	if scanner.Scan() {
		timestamp := scanner.Text()
		if _, err := time.Parse(time.RFC3339, timestamp); err == nil {
			r.Time = timestamp
			r.Message = strings.ReplaceAll(r.Message, timestamp, "")
		}
	}
	r.Message = strings.TrimSpace(r.Message)
	return r
}

// +kubebuilder:object:generate=false
type SearchAPI interface {
	Search(q *SearchParams) (r SearchResults, err error)
	MatchRoute(q *SearchParams) (match bool, isAdditive bool)
}

type SearchMapper interface {
	MapSearchParams(p *SearchParams) ([]SearchParams, error)
}
