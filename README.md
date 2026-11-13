# mdgo

A fast, single-binary Markdown website server built to be incredibly easy to use. Download mdgo, put your pages in `public/` and start it. No PHP, database, Go installation or separate frontend build tools are needed to run it.

mdgo is the successor to [mdcms](https://gitlab.com/coalaura/mdcms), a smaller PHP-based project with the same core idea: turn Markdown files into a website. Rewritten in Go, mdgo builds on that idea with more features and a focus on speed and simplicity.

Pages are rendered on demand and cached as HTML on disk, so subsequent requests skip Markdown parsing and template rendering. The default theme, admin interface and asset processor are built into the executable.

- **Markdown pages:** GitHub-flavored Markdown, YAML front matter, heading anchors, footnotes, definition lists and GitHub-style alerts.
- **Clean URLs:** Extensionless page paths and directory index pages, with configurable redirects.
- **Themes:** An embedded default theme with light and dark styling, plus custom Jet templates and error pages.
- **Asset processing:** Built-in esbuild integration for minification, fingerprinted CSS and JavaScript and optional JavaScript bundling.
- **Cache management:** A token-protected admin dashboard for clearing individual pages or rebuilding the whole site's cache.
- **Sitemaps:** An optional `/sitemap.xml` generated from routable Markdown pages.

## Contents

- [Quick start](#quick-start)
- [Writing pages](#writing-pages)
- [Configuration](#configuration)
- [Themes and assets](#themes-and-assets)
- [Admin and caching](#admin-and-caching)
- [Sitemap](#sitemap)
- [Serving with nginx](#serving-with-nginx)

## Quick start

1. Download the executable for your operating system and CPU architecture from [GitHub Releases](https://github.com/coalaura/mdgo/releases/latest). Extract it if packaged in an archive and place it in the directory you want to use for your site.

2. Create a `public/` directory and add `public/index.md`:

```markdown
---
title: Home
---

# Hello, world!

Welcome to my site.
```

3. Open a terminal in that directory and start the server. On Linux or macOS:

```sh
chmod +x mdgo
./mdgo
```

On Windows, run `./mdgo.exe` instead.

4. Open [http://localhost:2727](http://localhost:2727).

On first startup, mdgo creates `config.yml` if it is missing, generates an admin login token and publishes the default theme's assets. The default theme and admin interface are embedded in the executable.

All paths are relative to the process's working directory. Run mdgo from the directory containing your `config.yml` and `public/`; it needs write access to create configuration, cached pages and published assets.

### Building from source

To build mdgo yourself, install **Go 1.27.3 or newer**, as declared in `go.mod`:

```sh
git clone https://github.com/coalaura/mdgo.git
cd mdgo
go build -o mdgo .
./mdgo
```

On Windows, use `go build -o mdgo.exe .` and run `./mdgo.exe`.

## Writing pages

Place Markdown files and static files under `public/`. With the default configuration:

| File | URL |
| --- | --- |
| `public/index.md` | `/` |
| `public/about.md` | `/about` |
| `public/docs/index.md` | `/docs/` |
| `public/docs/install.md` | `/docs/install` |
| `public/images/logo.png` | `/images/logo.png` |
| `public/example.txt` | `/example.txt` |

Requests for `.md` pages redirect to their extensionless URLs. By default, `.html` URLs do the same and `index`, `index.html` and `index.md` URLs redirect to their directory. Directories receive a trailing slash.

### Front matter

Optional YAML front matter must appear at the very beginning of the file, enclosed by `---` lines. Its values are available to themes through `.page`:

```markdown
---
title: Getting started
description: A short introduction to the project.
---

# Getting started

Write your content here.
```

The default theme uses `title` for the browser tab title. Other fields, such as `description`, are available for custom themes to use.

### Markdown support

Alongside standard Markdown, mdgo supports tables, task lists, strikethrough, autolinks, footnotes, definition lists and automatic heading IDs. Raw HTML in Markdown is rendered as HTML.

GitHub-style alerts use the following syntax:

```markdown
> [!NOTE]
> A useful detail for your readers.
```

Supported alert types are `NOTE`, `TIP`, `IMPORTANT`, `WARNING` and `CAUTION`.

## Configuration

mdgo reads `config.yml` at startup. Missing settings use their defaults; restart the server after changing configuration.

```yaml
server:
  port: 2727
  secure: false

site:
  url: ""

theme:
  name: default
  assets:
    compile: false
    minify: true
    entrypoints: []

frontend:
  redirect_html: true
  redirect_index: true

tokens:
  auth: ""
```

| Setting | Purpose |
| --- | --- |
| `server.port` | HTTP listening port. The server binds to all interfaces. |
| `server.secure` | Sets the `Secure` flag on admin session cookies when the site is served over HTTPS. This does not enable TLS on the server; terminate TLS at a reverse proxy. |
| `site.url` | Public absolute HTTP(S) URL used for sitemap links. An empty value disables the sitemap. |
| `theme.name` | `default` for the embedded theme or a theme directory under `themes/`. |
| `theme.assets.compile` | Bundles declared JavaScript entrypoints and their dependencies. |
| `theme.assets.minify` | Minifies JavaScript and CSS. |
| `theme.assets.entrypoints` | Paths relative to the theme's `assets/` directory. Must be empty when compilation is disabled and nonempty when enabled. Each entry must end in `.js` or `.mjs`. |
| `frontend.redirect_html` | Redirects `.html` URLs to extensionless paths. |
| `frontend.redirect_index` | Redirects index URLs to their directory; takes precedence over `redirect_html`. |
| `tokens.auth` | Admin login token. If empty, mdgo generates a token and saves it to `config.yml`. |

## Themes and assets

The `default` theme is embedded at build time, so a release installation does not need a `themes/` directory. For a custom theme, use the [default theme sources](https://github.com/coalaura/mdgo/tree/master/themes/default) as a starting point, place your files in `themes/custom/` and set `theme.name: custom`.

```text
themes/
`-- custom/
    |-- layout.html
    |-- error.html
    `-- assets/
        |-- styles.css
        `-- app.js
```

`layout.html` is required; `error.html` and `assets/` are optional. Templates use Jet syntax:

| Template | Available data |
| --- | --- |
| `layout.html` | `.content`: rendered Markdown; `.page`: front matter fields. |
| `error.html` | `.status`: HTTP status code; `.message`: status text; `.path`: requested path. |

Use `{{ raw(.content) }}` to insert the rendered Markdown and `{{ .page.title }}` to read a front matter field. Guard optional fields with `isset(...)`, as the default layout does. Layouts are also evaluated without page content or metadata during theme loading.

If a custom error template is missing or fails, mdgo falls back to its built-in error page.

### Asset processing

Reference theme assets using their source paths, such as `/assets/styles.css` or `/assets/app.js`. mdgo publishes processed assets into `public/assets/` and rewrites managed references in rendered HTML to fingerprinted URLs.

By default, CSS and JavaScript are processed without enabling JavaScript entrypoint bundling. To bundle a custom theme's JavaScript and its dependencies, configure:

```yaml
theme:
  name: custom
  assets:
    compile: true
    minify: true
    entrypoints:
      - app.js
```

Here, `app.js` refers to `themes/custom/assets/app.js`. Asset processing uses esbuild's Go API, so mdgo itself does not require a separate JavaScript runtime or build command.

After editing a custom theme or its assets, clear all caches in the admin dashboard or restart the server to reload them. Changes to the embedded default theme require rebuilding the executable.

## Admin and caching

Open [http://localhost:2727/-/admin](http://localhost:2727/-/admin) and sign in with the `tokens.auth` value from `config.yml`.

The dashboard lists Markdown pages and their cache status. You can:

- **Clear one page:** Remove its cached HTML so the next request renders it again.
- **Clear all caches:** Reload the theme, rebuild its assets, clear cached HTML and remove obsolete managed assets.

Rendered HTML is stored beside its Markdown source: `public/about.md` produces `public/about.html`. Source edits do not automatically invalidate that cache. Clear the affected page after editing it or clear all caches after broader changes. Startup also rebuilds theme assets and clears cached pages.

Keep hand-authored HTML separate from Markdown files with the same basename: an `.html` file beside a matching `.md` file is treated as disposable cache. Standalone HTML and other static files can be served directly. The `/assets/` and `/-/` namespaces are reserved for asset delivery and the admin interface.

### Browser caching

- Managed fingerprinted assets use `public, max-age=31536000, immutable`.
- Other public responses use `no-cache`, requiring revalidation before reuse.
- Rendered pages and sitemaps support ETag-based revalidation.
- Admin responses, private responses and errors use `no-store`.

Clearing the server cache also invalidates page validators and sitemap data. It does not purge browser caches.

## Sitemap

Set `site.url` to your site's public URL to enable `/sitemap.xml`:

```yaml
site:
  url: https://example.com
```

The sitemap contains sorted, deduplicated URLs for routable Markdown pages, including the homepage. It excludes redirect URLs and the admin and asset namespaces. It is generated on demand and cached until a page cache clear, a full cache clear or a restart.

The URL must use HTTP or HTTPS and cannot contain credentials, a query or a fragment. A path prefix such as `https://example.com/docs` is retained in sitemap URLs; it does not change the server's routing or mount the application under that prefix.

## Serving with nginx

mdgo can serve a site on its own. For deployment behind nginx, let nginx serve existing static files directly and proxy page requests to mdgo. Images, stylesheets, scripts and downloads then bypass the Go server entirely.

The example below assumes mdgo runs from `/srv/mdgo` on port `2727`. Put this configuration inside nginx's `http` context, for example in `/etc/nginx/conf.d/mdgo.conf` and replace the domain and root path for your site. The nginx worker needs read access to `public/`.

```nginx
upstream mdgo {
    server 127.0.0.1:2727;
    keepalive 32;
}

server {
    listen 80;
    server_name example.com;

    root /srv/mdgo/public;

    include mime.types;

    default_type application/octet-stream;

    sendfile on;
    autoindex off;

    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_cache off;
    proxy_intercept_errors off;

    location ^~ /-/ {
        proxy_pass http://mdgo;
    }

    location = /sitemap.xml {
        proxy_pass http://mdgo;
    }

    # Keep internal manifests and temporary files private.
    location ~ (^|/)\. {
        return 404;
    }

    # Let mdgo handle source URLs and canonical page redirects.
    location ~* (\.md|\.html|/index)$ {
        proxy_pass http://mdgo;
    }

    location / {
        # Check only the exact file, leaving directory indexes to mdgo.
        try_files $uri @mdgo;
        add_header Cache-Control "no-cache";
    }

    location @mdgo {
        proxy_pass http://mdgo;
    }
}
```

| Request | Handled by |
| --- | --- |
| Existing `/assets/styles-ABC12345.css`, `/images/logo.png` or `/example.txt` | nginx, directly from `public/`. |
| `/`, `/about`, `/docs/` or a missing static file | mdgo, through the fallback. |
| `/about.md`, `/about.html` or `/docs/index` | mdgo, preserving its redirect settings and keeping page Markdown from being served as source. |
| `/-/admin`, `/-/login` and other `/-/` routes | mdgo. |
| `/sitemap.xml` | mdgo. |

Keep the fallback as `try_files $uri @mdgo`: adding `$uri/` or `$uri.html` would bypass mdgo's directory routing or rendered-page cache validators. Cached pages still avoid rendering work inside mdgo. Proxying here is internal; the browser only receives a redirect when mdgo's URL rules call for one.

nginx-served files use `no-cache` so browsers revalidate them after updates. This deliberately applies to all directly served files, including fingerprinted assets; the one-year immutable policy described above applies when mdgo serves its managed assets. Proxied responses retain mdgo's cache headers and proxy caching is disabled so admin cache clears take effect on the next request.

For HTTPS, add your certificate and TLS listener to nginx and set `server.secure: true` in `config.yml`. Set `site.url` to the public HTTPS URL if you want a sitemap. Keep port `2727` accessible only to the proxy; mdgo's listener binds to all interfaces.

Validate the configuration before reloading nginx:

```sh
sudo nginx -t
sudo nginx -s reload
```
