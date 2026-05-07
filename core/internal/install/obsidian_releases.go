package install

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const obsidianReleasesAPI = "https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest"

// obsidianAsset descreve um asset publicado no release mais recente do
// Obsidian. Mantemos um nome próprio para isolar callers do shape do JSON.
type obsidianAsset struct {
	Name string
	URL  string
}

// fetchLatestObsidianAssets retorna os assets do release mais recente,
// centralizando a chamada HTTP+decode usada pelos installers macOS/Linux.
// Antes da consolidação cada OS duplicava esta lógica e podia divergir
// (User-Agent, error wrapping, etc.).
func fetchLatestObsidianAssets() ([]obsidianAsset, error) {
	req, err := http.NewRequest("GET", obsidianReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dwyt-installer")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("obsidian release lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("obsidian release lookup HTTP %d", resp.StatusCode)
	}

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("obsidian release decode: %w", err)
	}
	out := make([]obsidianAsset, len(release.Assets))
	for i, a := range release.Assets {
		out[i] = obsidianAsset{Name: a.Name, URL: a.BrowserDownloadURL}
	}
	return out, nil
}
