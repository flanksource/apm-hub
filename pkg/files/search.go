package files

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/apm-hub/api/logs"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
)

func NewFileSearchBackend(config *logs.FileSearchBackendConfig) *FileSearch {
	return &FileSearch{
		config: config,
	}
}

type FileSearch struct {
	config *logs.FileSearchBackendConfig
}

func (t *FileSearch) Search(q *logs.SearchParams) (r logs.SearchResults, err error) {
	var res logs.SearchResults
	lines := readFilesLines(t.config.Paths, collections.MergeMap(t.config.Labels, q.Labels))
	for _, content := range lines {
		res.Results = append(res.Results, content...)
	}

	return res, nil
}

func (t *FileSearch) MatchRoute(q *logs.SearchParams) (match bool, isAdditive bool) {
	return t.config.Routes.MatchRoute(q)
}

type logsPerFile map[string][]logs.Result

// readFilesLines takes a list of file paths and returns each lines of those files.
// If labels are also passed, it'll attach those labels to each lines of those files.
func readFilesLines(paths []string, labelsToAttach map[string]string) logsPerFile {
	fileContents := make(logsPerFile, len(paths))
	for _, path := range unfoldGlobs(paths) {
		fInfo, err := os.Stat(path)
		if err != nil {
			logger.Warnf("error get file stat. path=%s; %w", path, err)
			continue
		}

		file, err := os.Open(path)
		if err != nil {
			logger.Warnf("error opening file. path=%s; %w", path, err)
			continue
		}

		// All lines of the same file will share these labels
		labels := collections.MergeMap(map[string]string{"path": path}, labelsToAttach)

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fileContents[path] = append(fileContents[path], logs.Result{
				Time:    fInfo.ModTime().Format(time.RFC3339),
				Labels:  labels,
				Message: strings.TrimSpace(scanner.Text()),
			})
		}
	}

	return fileContents
}

func unfoldGlobs(paths []string) []string {
	unfoldedPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		matched, err := filepath.Glob(path)
		if err != nil {
			logger.Warnf("invalid glob pattern. path=%s; %w", path, err)
			continue
		}

		unfoldedPaths = append(unfoldedPaths, matched...)
	}

	return unfoldedPaths
}
