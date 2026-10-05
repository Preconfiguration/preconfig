package doctor

import "sort"

// Doctor's knowledge: which Ubuntu 24.04 package provides a header, a shared
// library or a command, and what a download host is. Every package name here
// was checked against Ubuntu 24.04's archive by tools/doctor/check-packages.sh
// on the date below.

// KnowledgeDate is when the package names were last checked.
const KnowledgeDate = "2026-09-30"

// headerPackages maps a C header that a build can't find to the package that
// provides it.
var headerPackages = map[string]string{
	"Python.h":                "python3-dev",
	"libpq-fe.h":              "libpq-dev",
	"pg_config.h":             "libpq-dev",
	"ffi.h":                   "libffi-dev",
	"openssl/ssl.h":           "libssl-dev",
	"openssl/opensslv.h":      "libssl-dev",
	"zlib.h":                  "zlib1g-dev",
	"jpeglib.h":               "libjpeg-dev",
	"png.h":                   "libpng-dev",
	"libxml/xmlversion.h":     "libxml2-dev",
	"libxml/parser.h":         "libxml2-dev",
	"libxslt/xsltconfig.h":    "libxslt1-dev",
	"libxslt/xslt.h":          "libxslt1-dev",
	"mysql.h":                 "default-libmysqlclient-dev",
	"mysql/mysql.h":           "default-libmysqlclient-dev",
	"sqlite3.h":               "libsqlite3-dev",
	"yaml.h":                  "libyaml-dev",
	"curl/curl.h":             "libcurl4-openssl-dev",
	"krb5.h":                  "libkrb5-dev",
	"gssapi/gssapi.h":         "libkrb5-dev",
	"ldap.h":                  "libldap-dev",
	"lber.h":                  "libldap-dev",
	"sasl/sasl.h":             "libsasl2-dev",
	"bzlib.h":                 "libbz2-dev",
	"lzma.h":                  "liblzma-dev",
	"readline/readline.h":     "libreadline-dev",
	"uuid/uuid.h":             "uuid-dev",
	"magic.h":                 "libmagic-dev",
	"cairo.h":                 "libcairo2-dev",
	"gmp.h":                   "libgmp-dev",
	"systemd/sd-daemon.h":     "libsystemd-dev",
	"portaudio.h":             "portaudio19-dev",
	"ft2build.h":              "libfreetype-dev",
	"glib.h":                  "libglib2.0-dev",
	"hdf5.h":                  "libhdf5-dev",
	"zstd.h":                  "libzstd-dev",
	"expat.h":                 "libexpat1-dev",
	"gdbm.h":                  "libgdbm-dev",
	"ncurses.h":               "libncurses-dev",
	"curses.h":                "libncurses-dev",
	"pcre2.h":                 "libpcre2-dev",
	"libusb-1.0/libusb.h":     "libusb-1.0-0-dev",
	"alsa/asoundlib.h":        "libasound2-dev",
	"X11/Xlib.h":              "libx11-dev",
	"snappy-c.h":              "libsnappy-dev",
	"librdkafka/rdkafka.h":    "librdkafka-dev",
	"gdal.h":                  "libgdal-dev",
	"geos_c.h":                "libgeos-dev",
	"proj.h":                  "libproj-dev",
	"vips/vips.h":             "libvips-dev",
}

// libraryPackages maps a shared library that a program couldn't load, by the
// name it asked for, to the package that provides it.
var libraryPackages = map[string]string{
	"libGL.so.1":           "libgl1",
	"libgomp.so.1":         "libgomp1",
	"libglib-2.0.so.0":     "libglib2.0-0t64",
	"libgthread-2.0.so.0":  "libglib2.0-0t64",
	"libmagic.so.1":        "libmagic1t64",
	"libmagic":             "libmagic1t64",
	"magic":                "libmagic1t64",
	"libzbar.so.0":         "libzbar0t64",
	"zbar":                 "libzbar0t64",
	"libcairo.so.2":        "libcairo2",
	"cairo":                "libcairo2",
	"cairo-2":              "libcairo2",
	"libpango-1.0.so.0":    "libpango-1.0-0",
	"pango-1.0-0":          "libpango-1.0-0",
	"libxml2.so.2":         "libxml2",
	"libpq.so.5":           "libpq5",
	"libpq":                "libpq5",
	"pq":                   "libpq5",
	"libssl.so.3":          "libssl3t64",
	"libcrypto.so.3":       "libssl3t64",
	"libffi.so.8":          "libffi8",
	"libsndfile.so.1":      "libsndfile1",
	"sndfile":              "libsndfile1",
	"libvips.so.42":        "libvips42t64",
	"libsqlite3.so.0":      "libsqlite3-0",
	"libX11.so.6":          "libx11-6",
	"libxcb.so.1":          "libxcb1",
	"libnss3.so":           "libnss3",
	"libasound.so.2":       "libasound2t64",
	"libgdal.so":           "gdal-bin",
	"libgeos_c.so":         "libgeos-c1t64",
	"libtesseract.so.5":    "libtesseract5",
}

// commandPackages maps a command that isn't installed to the package that
// provides it on Ubuntu 24.04.
var commandPackages = map[string]string{
	"gcc": "build-essential", "cc": "build-essential", "g++": "build-essential", "c++": "build-essential",
	"x86_64-linux-gnu-gcc": "build-essential", "aarch64-linux-gnu-gcc": "build-essential",
	"make": "make", "cmake": "cmake", "pkg-config": "pkg-config",
	"sqlite3": "sqlite3", "psql": "postgresql-client", "redis-cli": "redis-tools",
	"unzip": "unzip", "zip": "zip", "jq": "jq", "wget": "wget", "rsync": "rsync",
	"ffmpeg": "ffmpeg", "ffprobe": "ffmpeg", "convert": "imagemagick", "magick": "imagemagick",
	"protoc": "protobuf-compiler", "git-lfs": "git-lfs", "xvfb-run": "xvfb",
	"tesseract": "tesseract-ocr", "pdftotext": "poppler-utils", "pdftoppm": "poppler-utils",
	"gs": "ghostscript", "dot": "graphviz", "xmllint": "libxml2-utils", "envsubst": "gettext-base",
	"msgfmt": "gettext", "zstd": "zstd", "xz": "xz-utils", "bzip2": "bzip2", "patch": "patch",
	"file": "file", "less": "less", "bc": "bc", "netcat": "netcat-openbsd", "nc": "netcat-openbsd",
	"ssh": "openssh-client", "scp": "openssh-client", "dig": "dnsutils", "gdal-config": "libgdal-dev",
	"pg_config": "libpq-dev",
}

// commandRuntimes maps a command to the runtime in the spec that provides it.
var commandRuntimes = map[string]string{
	"python3": "python", "python": "python", "pip": "python", "pip3": "python",
	"node": "node", "npm": "node", "npx": "node", "corepack": "node",
	"go": "go", "gofmt": "go",
}

// commandTools maps a command to the tool in the spec that provides it.
var commandTools = map[string]string{
	"uv": "uv", "uvx": "uv", "pnpm": "pnpm", "yarn": "yarn", "poetry": "poetry",
}

// hostNames says what a download host is, for the diagnosis.
var hostNames = []struct{ suffix, name, what string }{
	{"deb.nodesource.com", "NodeSource", "Node.js"},
	{"nodejs.org", "nodejs.org", "Node.js"},
	{"apt.postgresql.org", "the PostgreSQL project's archive", "PostgreSQL"},
	{"www.postgresql.org", "the PostgreSQL project's site", "the PostgreSQL archive's signing key"},
	{"packages.redis.io", "Redis's packages", "Redis"},
	{"proxy.golang.org", "Go's module proxy", "Go toolchains and modules"},
	{"golang.org", "Go's downloads", "Go"},
	{"go.dev", "Go's downloads", "Go"},
	{"dl.google.com", "Google's downloads", "Go"},
	{"github.com", "GitHub", "release downloads"},
	{"githubusercontent.com", "GitHub", "release downloads"},
	{"pypi.org", "PyPI", "Python packages"},
	{"files.pythonhosted.org", "PyPI", "Python packages"},
	{"registry.npmjs.org", "the npm registry", "npm packages"},
	{"registry.yarnpkg.com", "the Yarn registry", "npm packages"},
	{"archive.ubuntu.com", "Ubuntu's archive", "system packages"},
	{"security.ubuntu.com", "Ubuntu's security archive", "system packages"},
	{"ports.ubuntu.com", "Ubuntu's archive", "system packages"},
	{"docker.io", "Docker Hub", "container images"},
	{"ghcr.io", "GitHub's container registry", "container images"},
}

// hostName describes a host for a person: "NodeSource (deb.nodesource.com)".
func hostName(host string) (name, what string) {
	for _, h := range hostNames {
		if host == h.suffix || hasSuffix(host, "."+h.suffix) {
			return h.name, h.what
		}
	}
	return host, ""
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// packageNames are every package Doctor might suggest, for suggesting the
// right name when a spec misspells one.
func packageNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range headerPackages {
		add(p)
	}
	for _, p := range libraryPackages {
		add(p)
	}
	for _, p := range commandPackages {
		add(p)
	}
	for _, p := range []string{"locales-all", "locales", "python3-dev", "python3-venv", "python3-pip", "ca-certificates", "curl", "git", "gnupg", "tzdata", "libpq-dev", "build-essential"} {
		add(p)
	}
	sort.Strings(out)
	return out
}

// AllPackages lists every package name in Doctor's knowledge, for the check
// against the archive.
func AllPackages() []string { return packageNames() }
