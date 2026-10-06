//go:build linux

package install

import "encoding/json"

func pluginIDAt(dir string) string {
	root, err := openRoot(dir)
	if err != nil {
		return ""
	}
	defer root.Close()
	raw, err := root.ReadFile("manifest.json")
	if err != nil {
		return ""
	}
	var m struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return m.ID
}
