package web

// navItem is a documentation page shown in the sidebar navigation.
type navItem struct {
	Title string
	Path  string
	File  string
}

var navItems = []navItem{
	{Title: "Overview", Path: "/", File: "content/index.md"},
	{Title: "Go Modules", Path: "/docs/go", File: "content/go.md"},
	{Title: "Python (PyPI)", Path: "/docs/python", File: "content/python.md"},
	{Title: "Nix Binary Cache", Path: "/docs/nix", File: "content/nix.md"},
	{Title: "NPM", Path: "/docs/npm", File: "content/npm.md"},
	{Title: "Authentication", Path: "/docs/auth", File: "content/auth.md"},
	{Title: "Pushing Packages", Path: "/docs/push", File: "content/push.md"},
}
