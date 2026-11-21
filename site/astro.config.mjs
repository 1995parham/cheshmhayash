// @ts-check
import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";
import starlightLinksValidator from "starlight-links-validator";

export default defineConfig({
  site: "https://1995parham.me",
  base: "/cheshmhayash",
  integrations: [
    starlight({
      title: "cheshmhayash",
      description:
        "A Swiss Army knife for NATS: an admin dashboard, an MCP server, protobuf-aware pub/sub and safe JetStream operations in one binary.",
      favicon: "/favicon.svg",
      social: [
        { icon: "github", label: "GitHub", href: "https://github.com/1995parham/cheshmhayash" },
      ],
      editLink: {
        baseUrl: "https://github.com/1995parham/cheshmhayash/edit/main/site/",
      },
      lastUpdated: false,
      customCss: ["./src/styles/custom.css"],
      plugins: [
        // The demo lives under public/demo/ and is copied in by CI, so links
        // to it are not pages that the validator knows.
        starlightLinksValidator({ exclude: ["/cheshmhayash/demo/", "/cheshmhayash/demo/**"] }),
      ],
      sidebar: [
        {
          label: "Start here",
          items: [
            { label: "Introduction", slug: "getting-started" },
            { label: "Live demo", link: "/demo/", attrs: { target: "_blank" } },
          ],
        },
        {
          label: "Dashboard",
          items: [
            { label: "Overview", slug: "dashboard" },
            { label: "Servers", slug: "dashboard/servers" },
            { label: "Accounts", slug: "dashboard/accounts" },
            { label: "JetStream", slug: "dashboard/jetstream" },
            { label: "Health", slug: "dashboard/health" },
            { label: "Topology", slug: "dashboard/topology" },
            { label: "Tap", slug: "dashboard/tap" },
          ],
        },
        {
          label: "Command line",
          items: [
            { label: "Overview", slug: "cli" },
            { label: "sub and pub", slug: "cli/pub-sub" },
            { label: "schema", slug: "cli/schema" },
            { label: "consumer", slug: "cli/consumer" },
            { label: "peer and stream", slug: "cli/peer-stream" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Protobuf schemas", slug: "guides/schemas" },
            { label: "MCP server", slug: "guides/mcp" },
            { label: "Operations bot", slug: "guides/bot" },
            { label: "Deploy", slug: "guides/deploy" },
            { label: "Authentication", slug: "guides/auth" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "Configuration", slug: "reference/configuration" },
            { label: "HTTP API", slug: "reference/http-api" },
            { label: "MCP tools", slug: "reference/mcp-tools" },
          ],
        },
      ],
    }),
  ],
});
