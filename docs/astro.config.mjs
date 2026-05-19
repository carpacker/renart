import { defineConfig, fontProviders } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeRapide from 'starlight-theme-rapide';

const site = 'https://getrenart.com';
const socialImage = `${site}/landing/feature-dag-canvas.png`;
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
            { label: 'Renart for Bruin Users', slug: 'docs/bruin-cli-and-renart' },
          ],
        },
        {
          label: 'Getting Started',
          items: [
            { label: 'Installation', slug: 'docs/installation' },
            { label: 'Quickstart', slug: 'docs/quickstart' },
            { label: 'Running Renart', slug: 'docs/running-renart' },
          ],
        },
      ],
    }),
  ],
});
