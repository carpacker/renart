import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeRapide from 'starlight-theme-rapide';

export default defineConfig({
  integrations: [
    starlight({
      title: 'Renart Docs',
      logo: {
	src: '../web/public/icons/icon.svg',
      },
      plugins: [starlightThemeRapide()],
      description: 'User documentation for Renart, the visual workspace for Bruin projects.',
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
            { label: 'Who Renart Is For', slug: 'docs/who-renart-is-for' },
            { label: 'Bruin CLI and Renart', slug: 'docs/bruin-cli-and-renart' },
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
