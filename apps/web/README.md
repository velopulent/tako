# Tako web

Static marketing site built with Astro and Starlight. The landing page uses custom Astro components; Starlight serves the documentation placeholder at `/docs/`.

From the repository root:

```sh
bun install
bun nx dev web
bun nx build web
bun nx lint web
```

The development server runs at `http://localhost:4321`. The build writes static files to `apps/web/dist`, independent of the embedded administration dashboard. Serve that directory with any static host. Configure the host to serve `404.html` for missing pages.

Set `SITE_URL` to the public origin at build time to generate canonical URLs and a sitemap. Node.js 22.12 or newer is required by Astro 6.
