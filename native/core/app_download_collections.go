package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (manager *nativeDownloads) jobDirectory(job nativeDownloadJob) string {
	if job.Folder != "" {
		return filepath.Join(manager.root, filepath.FromSlash(job.Folder))
	}
	return filepath.Join(manager.root, job.ID)
}

func (manager *nativeDownloads) localFilesValid(record nativeDownloadRecord) bool {
	if record.File == "" {
		return false
	}
	directory := manager.jobDirectory(record.nativeDownloadJob)
	seen := map[string]bool{}
	var validate func(string, int) bool
	validate = func(name string, depth int) bool {
		if depth > 8 || len(seen) > 20100 || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") {
			return false
		}
		if seen[name] {
			return true
		}
		seen[name] = true
		file := filepath.Join(directory, name)
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return false
		}
		if name == "media.mp4" && record.Bytes > 0 && info.Size() != record.Bytes {
			return false
		}
		if !strings.HasSuffix(name, ".m3u8") {
			return true
		}
		if info.Size() > 4<<20 {
			return false
		}
		data, err := os.ReadFile(file)
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(data)), "#EXTM3U") {
			return false
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !validate(line, depth+1) {
				return false
			}
			for _, match := range nativePlaylistURI.FindAllStringSubmatch(line, -1) {
				if !validate(match[1], depth+1) {
					return false
				}
			}
		}
		return true
	}
	return validate(record.File, 0)
}

type nativeDownloadControlResult struct {
	Completed []string          `json:"completed"`
	Failures  map[string]string `json:"failures"`
}

func (manager *nativeDownloads) controlBatch(ids []string, command string) (nativeDownloadControlResult, error) {
	return manager.controlBatchExpected(context.Background(), ids, command, nil)
}

func (manager *nativeDownloads) controlBatchExpected(ctx context.Context, ids []string, command string, expected map[string]string) (nativeDownloadControlResult, error) {
	result := nativeDownloadControlResult{Completed: []string{}, Failures: map[string]string{}}
	if len(ids) == 0 || len(ids) > 500 {
		return result, errors.New("请选择 1 至 500 个下载任务")
	}
	switch command {
	case "pause", "resume", "remove", "archive", "restore":
	default:
		return result, errors.New("批量下载操作无效")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if ctx.Err() != nil {
			result.Failures[id] = "操作已停止，尚未处理此任务"
			continue
		}
		if expected != nil && expected[id] == "" {
			result.Failures[id] = "原分集校验信息缺失，已保留文件"
			continue
		}
		if err := manager.controlExpected(id, command, expected[id]); err != nil {
			result.Failures[id] = publicError(err).Error()
		} else {
			result.Completed = append(result.Completed, id)
		}
	}
	return result, nil
}
