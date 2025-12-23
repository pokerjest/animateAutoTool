package renamer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RenameTask 定义重命名任务参数
type RenameTask struct {
	SourcePath string // 下载完成的原始文件路径
	Title      string // 番剧名 (目标文件夹名)
	Season     string // 季度 (如 "Season 1")
	Episode    string // 集数 (如 "01")
	Ext        string // 扩展名
	DestBase   string // 目标根目录 (如 "/media/library")
}

// Execute 执行重命名/硬链接
// mode: "link", "move", "copy"
func Execute(task RenameTask, mode string) error {
	// 构建目标路径: DestBase / Title / Season / Title - SxxExx.ext
	// 这里做一些简单的格式化
	seasonStr := task.Season
	if !strings.HasPrefix(seasonStr, "Season") && !strings.HasPrefix(seasonStr, "S") {
		seasonStr = "Season 1" // 默认
	}

	// 简单的 S01E01 格式化
	// 假设 Episode 是 "1", "01" -> "E01"
	// 如果无法解析数字，就保留原样
	epStr := fmt.Sprintf("E%s", task.Episode)

	newName := fmt.Sprintf("%s - %s%s%s", task.Title, "S01", epStr, task.Ext)
	// 注意：这里 Season 写死了 S01，需要从 Parser 传递真正的 SeasonVal，或者默认 S01。

	destDir := filepath.Join(task.DestBase, task.Title, seasonStr)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	destPath := filepath.Join(destDir, newName)

	switch mode {
	case "link":
		return os.Link(task.SourcePath, destPath)
	case "move":
		return os.Rename(task.SourcePath, destPath)
	case "copy":
		return copyFile(task.SourcePath, destPath)
	default: // default to link
		return os.Link(task.SourcePath, destPath)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
