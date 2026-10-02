package legal

import (
	"embed"
	"os"
	"strings"
	"time"
)

//go:embed terms.md
var embedded embed.FS

type Document struct {
	Version   string
	Title     string
	Markdown  string
	UpdatedAt string
}

func Embedded() Document {
	raw, err := embedded.ReadFile("terms.md")
	if err != nil {
		return Document{Version: "1.0", Title: "用户协议与免责声明", UpdatedAt: "2026-10-02T00:00:00Z"}
	}
	return Parse(string(raw), "")
}

// Load reads path when the file exists, otherwise the copy baked into the binary.
// versionOverride replaces the version from the file when it is not empty.
func Load(path, versionOverride string) Document {
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			return Parse(string(raw), versionOverride)
		}
	}
	doc := Embedded()
	if strings.TrimSpace(versionOverride) != "" {
		doc.Version = strings.TrimSpace(versionOverride)
	}
	return doc
}

func Parse(raw, versionOverride string) Document {
	body := strings.ReplaceAll(raw, "\r\n", "\n")
	doc := Document{
		Version:   "1.0",
		Title:     "用户协议与免责声明",
		UpdatedAt: "2026-10-02T00:00:00Z",
		Markdown:  strings.TrimSpace(body),
	}
	if strings.HasPrefix(body, "---\n") {
		rest := strings.TrimPrefix(body, "---\n")
		front, markdown, ok := strings.Cut(rest, "\n---\n")
		if ok {
			doc.Markdown = strings.TrimSpace(markdown)
			for _, line := range strings.Split(front, "\n") {
				key, value, found := strings.Cut(line, ":")
				if !found {
					continue
				}
				key = strings.TrimSpace(key)
				value = strings.Trim(strings.TrimSpace(value), `"'`)
				switch key {
				case "version":
					if value != "" {
						doc.Version = value
					}
				case "title":
					if value != "" {
						doc.Title = value
					}
				case "updated":
					if value != "" {
						doc.UpdatedAt = normalizeDate(value)
					}
				}
			}
		}
	}
	if override := strings.TrimSpace(versionOverride); override != "" {
		doc.Version = override
	}
	return doc
}

func normalizeDate(value string) string {
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return value
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return value
}

// Seed writes the embedded terms into path when that file does not exist yet.
func Seed(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	raw, err := embedded.ReadFile("terms.md")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func dirOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "."
	}
	return path[:i]
}
