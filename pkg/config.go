package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	v8 "github.com/elastic/go-elasticsearch/v8"
	"github.com/flanksource/apm-hub/api/logs"
	"github.com/flanksource/apm-hub/db"
	"github.com/flanksource/apm-hub/pkg/cloudwatch"
	"github.com/flanksource/apm-hub/pkg/elasticsearch"
	"github.com/flanksource/apm-hub/pkg/files"
	k8s "github.com/flanksource/apm-hub/pkg/kubernetes"
	pkgOpensearch "github.com/flanksource/apm-hub/pkg/opensearch"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/kommons"
	"github.com/opensearch-project/opensearch-go/v2"
	"gopkg.in/yaml.v3"
)

// ParseConfig parses the config file and returns the SearchConfig
func ParseConfig(configFile string) (*logs.SearchConfig, error) {
	searchConfig := &logs.SearchConfig{
		Path: configFile,
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("error reading the configFile: %v", err)
	}

	if err := yaml.Unmarshal(data, searchConfig); err != nil {
		return nil, fmt.Errorf("error unmarshalling the configFile: %v", err)
	}

	return searchConfig, nil
}

// SetupBackends instantiates backends from the given configurations.
func SetupBackends(kommonsClient *kommons.Client, backendConfigs []logs.SearchBackendConfig) []logs.SearchBackend {
	var allBackends []logs.SearchBackend
	for _, config := range backendConfigs {
		backends, err := getBackendsFromConfigs(kommonsClient, config)
		if err != nil {
			logger.Errorf("error instantiating backend from the config: %v", err)
			continue
		}

		allBackends = append(allBackends, backends...)
	}
	return allBackends
}

func LoadGlobalBackends() error {
	kommonsClient, err := kommons.NewClientFromDefaults(logger.GetZapLogger())
	if err != nil {
		return fmt.Errorf("error getting the kommons client: %w", err)
	}

	dbBackendConfigs, err := db.GetLoggingBackendsSpecs()
	if err != nil {
		return fmt.Errorf("error getting the logging backend configs from the db: %w", err)
	}

	logs.GlobalBackends = SetupBackends(kommonsClient, dbBackendConfigs)
	return nil
}

var errRoutesNotProvided = fmt.Errorf("no routes provided")

// getBackendsFromConfigs instantiates backends from the given configuration.
//
// A single configuration can have multiple backends.
func getBackendsFromConfigs(kommonsClient *kommons.Client, backendConfig logs.SearchBackendConfig) ([]logs.SearchBackend, error) {
	var backends []logs.SearchBackend

	if backendConfig.Kubernetes != nil {
		if len(backendConfig.Kubernetes.Routes) == 0 {
			return nil, errRoutesNotProvided
		}

		k8sclient, err := k8s.GetKubeClient(kommonsClient, backendConfig.Kubernetes)
		if err != nil {
			return nil, err
		}

		backend := logs.NewSearchBackend(k8s.NewKubernetesSearchBackend(k8sclient, backendConfig.Kubernetes))
		backends = append(backends, backend)
	}

	if backendConfig.File != nil {
		if len(backendConfig.File.Routes) == 0 {
			return nil, errRoutesNotProvided
		}

		// If the paths are not absolute,
		// They should be parsed with respect to the current path
		for j, p := range backendConfig.File.Paths {
			if !filepath.IsAbs(p) {
				currentPath, _ := os.Getwd()
				backendConfig.File.Paths[j] = filepath.Join(currentPath, p)
			}
		}

		backend := logs.NewSearchBackend(files.NewFileSearchBackend(backendConfig.File))
		backends = append(backends, backend)
	}

	if backendConfig.ElasticSearch != nil {
		if len(backendConfig.ElasticSearch.Routes) == 0 {
			return nil, errRoutesNotProvided
		}

		cfg, err := getElasticConfig(kommonsClient, backendConfig.ElasticSearch)
		if err != nil {
			return nil, fmt.Errorf("error getting the elastic search config: %w", err)
		}

		esClient, err := v8.NewClient(*cfg)
		if err != nil {
			return nil, fmt.Errorf("error creating the elastic search client: %w", err)
		}

		pingResp, err := esClient.Ping()
		if err != nil {
			return nil, fmt.Errorf("error pinging the elastic search client: %w", err)
		}

		if pingResp.StatusCode != 200 {
			return nil, fmt.Errorf("[elasticsearch] got ping response: %d", pingResp.StatusCode)
		}

		es, err := elasticsearch.NewElasticSearchBackend(esClient, backendConfig.ElasticSearch)
		if err != nil {
			return nil, fmt.Errorf("error creating the elastic search backend: %w", err)
		}

		backend := logs.NewSearchBackend(es)
		backends = append(backends, backend)
	}

	if backendConfig.OpenSearch != nil {
		if len(backendConfig.OpenSearch.Routes) == 0 {
			return nil, errRoutesNotProvided
		}

		cfg, err := getOpenSearchConfig(kommonsClient, backendConfig.OpenSearch)
		if err != nil {
			return nil, fmt.Errorf("error getting the openSearch config: %w", err)
		}

		osClient, err := opensearch.NewClient(*cfg)
		if err != nil {
			return nil, fmt.Errorf("error creating the openSearch client: %w", err)
		}

		pingResp, err := osClient.Ping()
		if err != nil {
			return nil, fmt.Errorf("error pinging the openSearch client: %w", err)
		}

		if pingResp.StatusCode != 200 {
			return nil, fmt.Errorf("[opensearch] got ping response: %d", pingResp.StatusCode)
		}

		osBackend, err := pkgOpensearch.NewOpenSearchBackend(osClient, backendConfig.OpenSearch)
		if err != nil {
			return nil, fmt.Errorf("error creating the openSearch backend: %w", err)
		}

		backend := logs.NewSearchBackend(osBackend)
		backends = append(backends, backend)
	}

	if backendConfig.CloudWatch != nil {
		_, accessKey, err := kommonsClient.GetEnvValue(*backendConfig.CloudWatch.Auth.AccessKey, backendConfig.CloudWatch.Namespace)
		if err != nil {
			return nil, err
		}

		_, secretKey, err := kommonsClient.GetEnvValue(*backendConfig.CloudWatch.Auth.SecretKey, backendConfig.CloudWatch.Namespace)
		if err != nil {
			return nil, err
		}

		cfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
			config.WithRegion(backendConfig.CloudWatch.Auth.Region),
		)
		if err != nil {
			return nil, fmt.Errorf("error creating aws config: %w", err)
		}

		client := cloudwatchlogs.NewFromConfig(cfg)

		// Make a request to verify that the auth & log group is valid.
		resp, err := client.DescribeLogGroups(context.Background(), &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: &backendConfig.CloudWatch.LogGroup})
		if err != nil {
			return nil, fmt.Errorf("error querying log group: %w", err)
		}

		var logGroupExists bool
		for _, group := range resp.LogGroups {
			if *group.LogGroupName == backendConfig.CloudWatch.LogGroup {
				logGroupExists = true
				break
			}
		}

		if !logGroupExists {
			return nil, fmt.Errorf("log group %s does not exist", backendConfig.CloudWatch.LogGroup)
		}

		cloudwatch := cloudwatch.NewCloudWatchSearchBackend(backendConfig.CloudWatch, client)

		backend := logs.NewSearchBackend(cloudwatch)
		backends = append(backends, backend)
	}

	return backends, nil
}

func getOpenSearchEnvVars(client *kommons.Client, conf *logs.OpenSearchBackendConfig) (username, password string, err error) {
	if conf.Username != nil {
		_, username, err = client.GetEnvValue(*conf.Username, conf.Namespace)
		if err != nil {
			err = fmt.Errorf("error getting the username: %w", err)
			return
		}
	}

	if conf.Password != nil {
		_, password, err = client.GetEnvValue(*conf.Password, conf.Namespace)
		if err != nil {
			err = fmt.Errorf("error getting the password: %w", err)
			return
		}
	}

	return
}

func getElasticSearchEnvVars(kClient *kommons.Client, conf *logs.ElasticSearchBackendConfig) (cloudID, apiKey, username, password string, err error) {
	if conf.CloudID != nil {
		_, cloudID, err = kClient.GetEnvValue(*conf.CloudID, conf.Namespace)
		if err != nil {
			err = fmt.Errorf("error getting the cloudID: %w", err)
			return
		}
	}

	if conf.Username != nil {
		_, username, err = kClient.GetEnvValue(*conf.Username, conf.Namespace)
		if err != nil {
			err = fmt.Errorf("error getting the username: %w", err)
			return
		}
	}

	if conf.Password != nil {
		_, password, err = kClient.GetEnvValue(*conf.Password, conf.Namespace)
		if err != nil {
			err = fmt.Errorf("error getting the password: %w", err)
			return
		}
	}

	if conf.APIKey != nil {
		_, apiKey, err = kClient.GetEnvValue(*conf.APIKey, conf.Namespace)
		if err != nil {
			err = fmt.Errorf("error getting the apiKey: %w", err)
			return
		}
	}

	return
}

func getElasticConfig(kClient *kommons.Client, conf *logs.ElasticSearchBackendConfig) (*v8.Config, error) {
	cloudID, apiKey, username, password, err := getElasticSearchEnvVars(kClient, conf)
	if err != nil {
		return nil, fmt.Errorf("error getting the env vars: %w", err)
	}

	if conf.Address != "" && cloudID != "" {
		return nil, fmt.Errorf("provide either an address or a cloudID")
	}

	cfg := v8.Config{
		Username: username,
		Password: password,
	}

	if conf.Address != "" {
		cfg.Addresses = []string{conf.Address}
	} else if cloudID != "" {
		cfg.CloudID = cloudID
		cfg.APIKey = apiKey
	} else {
		return nil, fmt.Errorf("provide at least an address or a cloudID")
	}

	return &cfg, nil
}

func getOpenSearchConfig(kClient *kommons.Client, conf *logs.OpenSearchBackendConfig) (*opensearch.Config, error) {
	username, password, err := getOpenSearchEnvVars(kClient, conf)
	if err != nil {
		return nil, fmt.Errorf("error getting the env vars: %w", err)
	}

	if conf.Address == "" {
		return nil, fmt.Errorf("address is required for OpenSearch")
	}

	cfg := opensearch.Config{
		Username:  username,
		Password:  password,
		Addresses: []string{conf.Address},
	}

	return &cfg, nil
}
