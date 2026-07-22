// Package templates embeds the docker-compose templates for each module, and
// Traefik's dynamic (file-provider) route configs, so they ship inside the
// single nullwatch binary with no external files required at runtime.
package templates

import "embed"

//go:embed adguard-compose.yml.tmpl wireguard-compose.yml.tmpl traefik-compose.yml.tmpl traefik-dynamic-casaos.yml.tmpl traefik-dynamic-app.yml.tmpl
var FS embed.FS
