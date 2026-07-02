package cloudwatch

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/flanksource/apm-hub/api/logs"
)

func NewCloudWatchSearchBackend(config *logs.CloudWatchBackendConfig, client *cloudwatchlogs.Client) *cloudWatchSearch {
	return &cloudWatchSearch{
		client: client,
		config: config,
	}
}

type cloudWatchSearch struct {
	client *cloudwatchlogs.Client
	config *logs.CloudWatchBackendConfig
}

func (t *cloudWatchSearch) MatchRoute(q *logs.SearchParams) (match bool, isAdditive bool) {
	return t.config.Routes.MatchRoute(q)
}

func (t *cloudWatchSearch) Search(q *logs.SearchParams) (logs.SearchResults, error) {
	logFilter := &cloudwatchlogs.StartQueryInput{
		LogGroupName: &t.config.LogGroup,
		Limit:        ptr(int32(q.Limit)),
		QueryString:  &t.config.Query,
	}

	if q.GetStart() != nil {
		logFilter.StartTime = ptr(q.GetStart().UnixMilli())
	}

	if q.GetEnd() != nil {
		logFilter.EndTime = ptr(q.GetEnd().UnixMilli())
	} else {
		logFilter.EndTime = ptr(time.Now().UnixMilli()) // end time is a required field
	}

	var result logs.SearchResults
	queryOutput, err := t.client.StartQuery(context.Background(), logFilter)
	if err != nil {
		return result, err
	}

	queryResult, err := t.getQueryResults(queryOutput.QueryId)
	if err != nil {
		return result, err
	}

	result.Total = int(queryResult.Statistics.RecordsMatched)

	result.Results = make([]logs.Result, 0, len(queryResult.Results))
	for _, fields := range queryResult.Results {
		var event = logs.Result{
			Labels: t.config.Labels,
		}

		for _, field := range fields {
			switch deref(field.Field) {
			case "@message":
				event.Message = deref(field.Value)
			case "@timestamp":
				event.Time = toRFC339(deref(field.Value))
			case "@ptr": // the value to use as logRecordPointer to retrieve that complete log event record.
				event.Id = deref(field.Value)
			case "":
				// Do nothing
			default:
				event.Labels[deref(field.Field)] = deref(field.Value)
			}
		}

		result.Results = append(result.Results, event)
	}

	return result, nil
}

func (t *cloudWatchSearch) getQueryResults(queryID *string) (*cloudwatchlogs.GetQueryResultsOutput, error) {
	input := &cloudwatchlogs.GetQueryResultsInput{
		QueryId: queryID,
	}

	for {
		resp, err := t.client.GetQueryResults(context.Background(), input)
		if err != nil {
			return nil, err
		}

		switch resp.Status {
		case types.QueryStatusComplete:
			return resp, nil
		case types.QueryStatusFailed:
			return nil, fmt.Errorf("query failed")
		case types.QueryStatusTimeout:
			return nil, fmt.Errorf("query timedout")
		case types.QueryStatusCancelled:
			return nil, fmt.Errorf("query cancelled")
		default:
			// Might be scheduling or running.
			// Wait before retrying.
			time.Sleep(time.Second)
		}
	}
}

// timestamp layout returned by Cloudwatch
const timestampLayout = "2006-01-02 15:04:05.000"

// Converts the timestamp returned by Cloudwatch
// to RFC3339 format.
func toRFC339(input string) string {
	t, err := time.Parse(timestampLayout, input)
	if err != nil {
		return ""
	}

	return t.Format(time.RFC3339)
}

func ptr[T any](val T) *T {
	return &val
}

func deref[T any](val *T) T {
	if val == nil {
		var v T
		return v
	}

	return *val
}
