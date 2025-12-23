package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type LocalAnimeData struct {
	SkipLayout  bool
	Directories []model.LocalAnimeDirectory
	AnimeList   []model.LocalAnime
}

// LocalAnimePageHandler 渲染本地番剧管理页面
func LocalAnimePageHandler(c *gin.Context) {
	skip := isHTMX(c)

	var dirs []model.LocalAnimeDirectory
	db.DB.Find(&dirs)

	var animes []model.LocalAnime
	db.DB.Find(&animes) // TODO: Pagination? For now fetch all

	data := LocalAnimeData{
		SkipLayout:  skip,
		Directories: dirs,
		AnimeList:   animes,
	}

	c.HTML(http.StatusOK, "local_anime.html", data)
}

// AddLocalDirectoryHandler 添加新的目录
func AddLocalDirectoryHandler(c *gin.Context) {
	path := c.PostForm("path")
	if path == "" {
		c.String(http.StatusBadRequest, "路径不能为空")
		return
	}

	svc := service.NewLocalAnimeService()
	if err := svc.AddDirectory(path); err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("添加失败: %v", err))
		return
	}

	// Trigger immediate scan
	go svc.ScanAll()

	time.Sleep(500 * time.Millisecond) // Wait a bit for UI
	c.Header("HX-Redirect", "/local-anime")
	c.Status(http.StatusOK)
}

// DeleteLocalDirectoryHandler 删除目录
func DeleteLocalDirectoryHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	svc := service.NewLocalAnimeService()
	if err := svc.RemoveDirectory(uint(id)); err != nil {
		c.String(http.StatusInternalServerError, "删除失败")
		return
	}

	c.Status(http.StatusOK)
}

// ScanLocalDirectoryHandler 触发重新扫描
func ScanLocalDirectoryHandler(c *gin.Context) {
	svc := service.NewLocalAnimeService()
	go svc.ScanAll()

	c.String(http.StatusOK, "扫描已在后台启动")
}
