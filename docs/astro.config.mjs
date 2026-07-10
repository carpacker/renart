import { defineConfig, fontProviders } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeRapide from 'starlight-theme-rapide';

const site = 'https://getrenart.com';
const socialImage = `${site}/landing/og-image.png`;
const siteDescription = 'Renart is the git-native data pipeline IDE for Bruin projects: a visual canvas, editor, inspect, and execution workflow for version-controlled pipelines.';
const docsDescription = 'Documentation for Renart, the git-native data pipeline IDE for visually editing, inspecting, running, and understanding Bruin projects.';

const docsStructuredData = JSON.stringify({
  '@context': 'https://schema.org',
  '@type': 'WebSite',
  name: 'Renart Docs',
  url: `${site}/docs/`,
  description: docsDescription,
  publisher: {
    '@type': 'Organization',
    name: 'Renart',
    url: site,
    logo: `${site}/icons/icon.svg`,
  },
});

export default defineConfig({
  site,
  fonts: [
    {
      provider: fontProviders.google(),
      name: 'Geist',
      cssVariable: '--font-geist',
      weights: ['400', '500', '600'],
      styles: ['normal'],
      subsets: ['latin'],
      display: 'swap',
      fallbacks: ['system-ui', 'sans-serif'],
    },
    {
      provider: fontProviders.google(),
      name: 'Geist Mono',
      cssVariable: '--font-geist-mono',
      weights: ['400', '500'],
      styles: ['normal'],
      subsets: ['latin'],
      display: 'swap',
      fallbacks: ['ui-monospace', 'monospace'],
    },
    {
      provider: fontProviders.google(),
      name: 'Instrument Serif',
      cssVariable: '--font-instrument-serif',
      weights: ['400'],
      styles: ['normal', 'italic'],
      subsets: ['latin'],
      display: 'swap',
      fallbacks: ['Georgia', 'serif'],
    },
  ],
  integrations: [
    starlight({
      title: 'Renart Docs',
      components: {
        Head: './src/components/Head.astro',
      },
      logo: {
	src: '../web/public/icons/icon.svg',
      },
      favicon: '/icons/icon.svg',
      plugins: [starlightThemeRapide()],
      description: docsDescription,
      head: [
        { tag: 'meta', attrs: { name: 'application-name', content: 'Renart' } },
        { tag: 'meta', attrs: { name: 'apple-mobile-web-app-title', content: 'Renart' } },
        { tag: 'meta', attrs: { name: 'theme-color', content: '#0f172a' } },
        { tag: 'meta', attrs: { name: 'keywords', content: 'Renart, Bruin, data pipeline IDE, git-native data pipelines, data engineering, analytics engineering, lineage canvas' } },
        { tag: 'meta', attrs: { name: 'author', content: 'Renart' } },
        { tag: 'meta', attrs: { property: 'og:type', content: 'website' } },
        { tag: 'meta', attrs: { property: 'og:site_name', content: 'Renart Docs' } },
        { tag: 'meta', attrs: { property: 'og:description', content: siteDescription } },
        { tag: 'meta', attrs: { property: 'og:locale', content: 'en_US' } },
        { tag: 'meta', attrs: { property: 'og:image', content: socialImage } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '675' } },
        { tag: 'meta', attrs: { property: 'og:image:alt', content: 'Renart DAG canvas showing connected Bruin assets' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        { tag: 'meta', attrs: { name: 'twitter:title', content: 'Renart Docs' } },
        { tag: 'meta', attrs: { name: 'twitter:description', content: docsDescription } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: socialImage } },
        { tag: 'meta', attrs: { name: 'twitter:image:alt', content: 'Renart DAG canvas showing connected Bruin assets' } },
        { tag: 'link', attrs: { rel: 'sitemap', type: 'application/xml', href: `${site}/sitemap-index.xml` } },
        { tag: 'script', attrs: { type: 'application/ld+json' }, content: docsStructuredData },
      ],
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/renart-data/renart',
        },
      ],
      sidebar: [
        {
          label: 'Introduction',
          items: [
            { label: 'Overview', slug: 'docs' },
            { label: 'Concepts', slug: 'docs/concepts' },
            { label: 'Who Renart Is For', slug: 'docs/who-renart-is-for' },
            { label: 'How Renart Works', slug: 'docs/introduction/how-renart-works' },
            { label: 'Renart & the Bruin CLI', slug: 'docs/bruin-cli-and-renart' },
          ],
        },
        {
          label: 'Getting Started',
          items: [
            { label: 'Installation', slug: 'docs/installation' },
            { label: 'Quickstart', slug: 'docs/quickstart' },
            { label: 'Build Your First Pipeline', slug: 'docs/getting-started/build-your-first-pipeline' },
            { label: 'Running Renart', slug: 'docs/running-renart' },
          ],
        },
        {
          label: 'The Workspace',
          items: [
            { label: 'Tour of the Interface', slug: 'docs/workspace/interface-tour' },
            { label: 'The Pipeline Canvas', slug: 'docs/workspace/pipeline-canvas' },
            { label: 'The Asset Catalog', slug: 'docs/workspace/asset-catalog' },
            { label: 'Runs & History', slug: 'docs/workspace/runs-and-history' },
          ],
        },
        {
          label: 'Editing Assets',
          items: [
            { label: 'The Asset Editor', slug: 'docs/editing-assets/asset-editor' },
            { label: 'The Asset Workbench', slug: 'docs/editing-assets/asset-workbench' },
            { label: 'Organising Assets into Folders', slug: 'docs/editing-assets/organising-assets' },
            { label: 'Identity, Owners & Tags', slug: 'docs/editing-assets/identity-owners-tags' },
            { label: 'Materialization Strategies', slug: 'docs/editing-assets/materialization' },
            { label: 'Dependencies', slug: 'docs/editing-assets/dependencies' },
            { label: 'Columns', slug: 'docs/editing-assets/columns' },
            { label: 'Quality Checks', slug: 'docs/editing-assets/quality-checks' },
            { label: 'How Provenance Works', slug: 'docs/editing-assets/provenance' },
          ],
        },
        {
          label: 'Asset Types',
          items: [
            { label: 'Choosing an Asset Type', slug: 'docs/asset-types/choosing-an-asset-type' },
            { label: 'SQL Assets', slug: 'docs/asset-types/sql-assets' },
            { label: 'Python Assets', slug: 'docs/asset-types/python-assets' },
            { label: 'HTTP API Assets', slug: 'docs/asset-types/http-api-assets' },
            { label: 'Load Assets', slug: 'docs/asset-types/load-assets' },
            { label: 'Seeds', slug: 'docs/asset-types/seeds' },
          ],
        },
        {
          label: 'Connections & Environments',
          items: [
            { label: 'Managing Connections', slug: 'docs/connections-environments/managing-connections' },
            { label: 'Environments', slug: 'docs/connections-environments/environments' },
            { label: 'Supported Connection Types', slug: 'docs/connections-environments/supported-connection-types' },
          ],
        },
        {
          label: 'Notebooks',
          collapsed: true,
          items: [
            { label: 'Notebooks Overview', slug: 'docs/notebooks/overview' },
            { label: 'Working in a Notebook', slug: 'docs/notebooks/working-in-a-notebook' },
            { label: 'Promoting Cells to Assets', slug: 'docs/notebooks/promoting-cells' },
          ],
        },
        {
          label: 'Scheduling',
          collapsed: true,
          items: [
            { label: 'Scheduling Overview', slug: 'docs/scheduling/overview' },
            { label: 'Creating & Editing Schedules', slug: 'docs/scheduling/creating-schedules' },
          ],
        },
        {
          label: 'Validation & Quality',
          collapsed: true,
          items: [
            { label: 'Type Checking in the UI', slug: 'docs/validation-quality/type-checking' },
            { label: 'SQL Validation & IntelliSense', slug: 'docs/validation-quality/sql-validation' },
          ],
        },
        {
          label: 'Automation & Deployment',
          collapsed: true,
          items: [
            { label: 'renart type-check in CI', slug: 'docs/automation-deployment/type-check-in-ci' },
            { label: 'Deploying with renart deploy', slug: 'docs/automation-deployment/deploy' },
            { label: 'Standalone Mode', slug: 'docs/automation-deployment/standalone' },
          ],
        },
        {
          label: 'Reference',
          collapsed: true,
          items: [
            { label: 'CLI Reference', slug: 'docs/reference/cli' },
            { label: 'Asset File Format', slug: 'docs/reference/asset-file-format' },
            { label: 'Keyboard Shortcuts', slug: 'docs/reference/keyboard-shortcuts' },
            { label: 'Configuration & Settings', slug: 'docs/reference/configuration' },
            { label: 'Troubleshooting & FAQ', slug: 'docs/reference/troubleshooting' },
          ],
        },
      ],
    }),
  ],
});
