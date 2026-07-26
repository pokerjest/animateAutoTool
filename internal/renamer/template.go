package renamer

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// DefaultSeriesTemplate and DefaultEpisodeTemplate follow the common TV
// library layout recognized by Jellyfin and Plex.
const (
	DefaultSeriesTemplate  = "{title}"
	DefaultEpisodeTemplate = "{title} - S{season}E{episode}{ext}"
)

var templateTokenPattern = regexp.MustCompile(`\{[a-z_]+\}`)

// TemplateData contains the stable values available to an automatic rename
// template. Ext includes the leading dot.
type TemplateData struct {
	Title    string
	Season   string
	Episode  string
	Year     string
	Ext      string
	Original string
}

// FormatTemplate renders a safe qBittorrent-relative media path. It rejects
// absolute/traversal paths and sanitizes every segment for Windows, macOS and
// Linux media libraries.
func FormatTemplate(pattern string, data TemplateData) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = DefaultEpisodeTemplate
	}

	values := map[string]string{
		"{title}":    sanitizeSegment(data.Title),
		"{season}":   normalizeNumber(data.Season, "01"),
		"{episode}":  normalizeEpisode(data.Episode),
		"{year}":     sanitizeSegment(data.Year),
		"{ext}":      normalizeExtension(data.Ext),
		"{original}": sanitizeSegment(data.Original),
	}
	if values["{title}"] == "" {
		return "", errors.New("rename title is empty")
	}
	if strings.Contains(pattern, "{episode}") && values["{episode}"] == "" {
		return "", errors.New("rename episode is empty")
	}

	for _, token := range templateTokenPattern.FindAllString(pattern, -1) {
		if _, ok := values[token]; !ok {
			return "", fmt.Errorf("unsupported rename token %s", token)
		}
	}
	for token, value := range values {
		pattern = strings.ReplaceAll(pattern, token, value)
	}
	pattern = strings.ReplaceAll(pattern, "()", "")

	pattern = strings.ReplaceAll(pattern, `\`, "/")
	if strings.HasPrefix(pattern, "/") || looksLikeWindowsAbsolutePath(pattern) {
		return "", errors.New("rename template must be a relative path")
	}

	parts := strings.Split(pattern, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", errors.New("rename template cannot leave the subscription directory")
		}
		part = sanitizeSegment(part)
		if part == "" {
			return "", errors.New("rename template contains an empty path segment")
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		return "", errors.New("rename template produced an empty path")
	}

	result := path.Clean(strings.Join(cleaned, "/"))
	if result == "." || result == ".." || strings.HasPrefix(result, "../") {
		return "", errors.New("rename template produced an unsafe path")
	}
	if path.Ext(result) == "" && values["{ext}"] != "" {
		result += values["{ext}"]
	}
	return result, nil
}

// ValidateTemplate checks custom input using representative media metadata.
func ValidateTemplate(pattern string) error {
	_, err := FormatTemplate(pattern, TemplateData{
		Title:    "示例番剧",
		Season:   "1",
		Episode:  "2",
		Year:     "2026",
		Ext:      ".mkv",
		Original: "[Group] Example - 02",
	})
	return err
}

// ValidateSeriesTemplate accepts only a single directory name so a custom
// template cannot escape the configured media root.
func ValidateSeriesTemplate(pattern string) error {
	if !strings.Contains(pattern, "{title}") {
		return errors.New("series template must contain {title}")
	}
	for _, token := range templateTokenPattern.FindAllString(pattern, -1) {
		if token != "{title}" && token != "{year}" {
			return fmt.Errorf("series template does not support %s", token)
		}
	}
	result, err := FormatTemplate(pattern, TemplateData{Title: "示例番剧", Year: "2026"})
	if err != nil {
		return err
	}
	if strings.Contains(result, "/") {
		return errors.New("series template must produce one directory name")
	}
	return nil
}

// ValidateEpisodeTemplate keeps the fixed Series/Season hierarchy intact and
// requires the fields media servers need to identify an episode.
func ValidateEpisodeTemplate(pattern string) error {
	for _, token := range []string{"{title}", "{season}", "{episode}", "{ext}"} {
		if !strings.Contains(pattern, token) {
			return fmt.Errorf("episode template must contain %s", token)
		}
	}
	result, err := FormatTemplate(pattern, TemplateData{Title: "示例番剧", Season: "1", Episode: "2", Year: "2026", Ext: ".mkv"})
	if err != nil {
		return err
	}
	if strings.Contains(result, "/") {
		return errors.New("episode template must produce one file name")
	}
	return nil
}

func normalizeNumber(value, fallback string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), "S"))
	if value == "" {
		return fallback
	}
	if number, err := strconv.Atoi(value); err == nil && number >= 0 {
		return fmt.Sprintf("%02d", number)
	}
	return sanitizeSegment(value)
}

func normalizeEpisode(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), "E"))
	if value == "" {
		return ""
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil && number >= 0 {
		if number == float64(int64(number)) {
			return fmt.Sprintf("%02d", int64(number))
		}
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return sanitizeSegment(value)
}

func normalizeExtension(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return strings.ToLower(sanitizeSegment(ext))
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsControl(r):
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	value = strings.Join(strings.Fields(b.String()), " ")
	value = strings.TrimRight(value, ". ")
	switch strings.ToUpper(value) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		value += "_"
	}
	return value
}

func looksLikeWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/'
}
