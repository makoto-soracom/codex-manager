package assets

import _ "embed"

//go:embed codex-manager.ico
var faviconICO []byte

//go:embed codex-manager-256.png
var iconPNG []byte

// FaviconICO returns the embedded Windows icon for browser favicon requests.
func FaviconICO() []byte {
	return faviconICO
}

// IconPNG returns the embedded 256x256 PNG app icon.
func IconPNG() []byte {
	return iconPNG
}
