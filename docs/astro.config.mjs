import { defineConfig, fontProviders } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeRapide from 'starlight-theme-rapide';

export default defineConfig({
  site: 'https://getrenart.com',
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
      logo: {
	src: '../web/public/icons/icon.svg',
      },
      plugins: [starlightThemeRapide()],
      description: 'User documentation for Renart, the IDE for git-native data pipelines.',
      head: [
        { tag: 'meta', attrs: { property: 'og:type', content: 'website' } },
        { tag: 'meta', attrs: { property: 'og:site_name', content: 'Renart Docs' } },
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://getrenart.com/landing/feature-dag-canvas.png' } },
        { tag: 'meta', attrs: { property: 'og:image:alt', content: 'Renart DAG canvas showing connected Bruin assets' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: 'https://getrenart.com/landing/feature-dag-canvas.png' } },
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
