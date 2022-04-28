package gateway

import (
	"strings"

	ipfspath "github.com/ipfs/go-path"
)

// structs for directory listing
type listingTemplateData struct {
	GatewayURL  string
	DNSLink     bool
	Listing     []directoryItem
	Size        string
	Path        string
	Breadcrumbs []breadcrumb
	BackLink    string
	Hash        string
}

type directoryItem struct {
	Size      string
	Name      string
	Path      string
	Hash      string
	ShortHash string
}

type breadcrumb struct {
	Name string
	Path string
}

func breadcrumbs(urlPath string, dnslinkOrigin bool) []breadcrumb {
	var ret []breadcrumb

	p, err := ipfspath.ParsePath(urlPath)
	if err != nil {
		// No breadcrumbs, fallback to bare Path in template
		return ret
	}
	segs := p.Segments()
	contentRoot := segs[1]
	for i, seg := range segs {
		if i == 0 {
			ret = append(ret, breadcrumb{Name: seg})
		} else {
			ret = append(ret, breadcrumb{
				Name: seg,
				Path: "/" + strings.Join(segs[0:i+1], "/"),
			})
		}
	}

	// Drop the /ipns/<fqdn> prefix from breadcrumb Paths when directory
	// listing on a DNSLink website (loaded due to Host header in HTTP
	// request).  Necessary because the hostname most likely won't have a
	// public gateway mounted.
	if dnslinkOrigin {
		prefix := "/ipns/" + contentRoot
		for i, crumb := range ret {
			if strings.HasPrefix(crumb.Path, prefix) {
				ret[i].Path = strings.Replace(crumb.Path, prefix, "", 1)
			}
		}
		// Make contentRoot breadcrumb link to the website root
		ret[1].Path = "/"
	}

	return ret
}

func shortHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return (hash[0:4] + "\u2026" + hash[len(hash)-4:])
}
