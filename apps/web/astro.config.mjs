import starlight from "@astrojs/starlight"
import { defineConfig } from "astro/config"
import icon from "astro-icon"

export default defineConfig({
  output: "static",
  site: process.env.SITE_URL,
  devToolbar: { enabled: false },
  integrations: [
    icon(),
    starlight({
      title: "Tako",
      components: { PageFrame: "./src/components/docs-frame.astro" },
      favicon: "/favicon.svg",
      disable404Route: true,
      pagefind: false,
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/velopulent/tako",
        },
      ],
      customCss: ["./src/styles/docs.css"],
    }),
  ],
})
