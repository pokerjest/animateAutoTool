package downloader

// Downloader 定义下载器通用接口
type Downloader interface {
	Login(username, password string) error
	// AddTorrent 添加下载任务
	// url: 磁力链或种子http地址
	// savePath: 下载保存路径
	// category: 分类(用于自动管理，即 tag)
	// paused: 是否添加为暂停状态
	AddTorrent(url, savePath, category string, paused bool) error

	// 简单的连通性测试
	Ping() error
}
