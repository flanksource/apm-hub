package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/flanksource/apm-hub/api/logs"
	"github.com/flanksource/apm-hub/external/elasticsearch"
	"github.com/flanksource/commons/logger"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
)

type OpenSearchBackend struct {
	client   *opensearch.Client
	fields   logs.ElasticSearchFields
	template *template.Template
	index    string
	config   *logs.OpenSearchBackendConfig
}

func NewOpenSearchBackend(client *opensearch.Client, config *logs.OpenSearchBackendConfig) (*OpenSearchBackend, error) {
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

	return &OpenSearchBackend{
		fields:   config.Fields,
		client:   client,
		config:   config,
		index:    config.Index,
		template: template,
	}, nil
}

func (t *OpenSearchBackend) MatchRoute(q *logs.SearchParams) (match bool, isAdditive bool) {
	return t.config.Routes.MatchRoute(q)
}

func (t *OpenSearchBackend) Search(q *logs.SearchParams) (logs.SearchResults, error) {
	var result logs.SearchResults
	var buf bytes.Buffer

	if err := t.template.Execute(&buf, q); err != nil {
		return result, fmt.Errorf("error executing template: %w", err)
	}
	logger.Debugf("Query: %s", buf.String())

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

	var r elasticsearch.SearchResponse
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return result, fmt.Errorf("error parsing the response body: %w", err)
	}

	result.Results = r.Hits.GetResultsFromHits(q.Limit, t.fields.Message, t.fields.Timestamp, t.config.Labels, t.fields.Exclusions...)
	result.Total = int(r.Hits.Total.Value)
	result.NextPage = r.Hits.NextPage(int(q.Limit))
	return result, nil
}
