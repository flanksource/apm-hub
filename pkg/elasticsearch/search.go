package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/flanksource/apm-hub/api/logs"
	pkgElasticsearch "github.com/flanksource/apm-hub/external/elasticsearch"
)

type ElasticSearchBackend struct {
	client   *elasticsearch.Client
	fields   logs.ElasticSearchFields
	template *template.Template
	index    string
	config   *logs.ElasticSearchBackendConfig
}

func NewElasticSearchBackend(client *elasticsearch.Client, config *logs.ElasticSearchBackendConfig) (*ElasticSearchBackend, error) {
	if client == nil {
		return nil, fmt.Errorf("client is nil")
	}

	if config.Index == "" {
		return nil, fmt.Errorf("index is empty")
	}

	template, err := template.New("query").Parse(config.Query)
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %w", err)
	}

	return &ElasticSearchBackend{
		client:   client,
		index:    config.Index,
		fields:   config.Fields,
		template: template,
		config:   config,
	}, nil
}

func (t *ElasticSearchBackend) MatchRoute(q *logs.SearchParams) (match bool, isAdditive bool) {
	return t.config.Routes.MatchRoute(q)
}

func (t *ElasticSearchBackend) Search(q *logs.SearchParams) (logs.SearchResults, error) {
	var result logs.SearchResults
	var buf bytes.Buffer

	if err := t.template.Execute(&buf, q); err != nil {
		return result, fmt.Errorf("error executing template: %w", err)
	}

	res, err := t.client.Search(
		t.client.Search.WithContext(context.Background()),
		t.client.Search.WithIndex(t.index),
		t.client.Search.WithBody(&buf),
		t.client.Search.WithSize(int(q.Limit+1)),
		t.client.Search.WithErrorTrace(),
	)
	if err != nil {
		return result, fmt.Errorf("error searching: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	var r pkgElasticsearch.SearchResponse
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return result, fmt.Errorf("error parsing the response body: %w", err)
	}

	result.Results = r.Hits.GetResultsFromHits(q.Limit, t.fields.Message, t.fields.Timestamp, t.config.Labels, t.fields.Exclusions...)
	result.Total = int(r.Hits.Total.Value)
	result.NextPage = r.Hits.NextPage(int(q.Limit))
	return result, nil
}
