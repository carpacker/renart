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
            { label: 'Overview', slug: 'index' },
            { label: 'Who Renart Is For', slug: 'who-renart-is-for' },
            { label: 'Bruin CLI and Renart', slug: 'bruin-cli-and-renart' },
          ],
        },
        {
          label: 'Getting Started',
          items: [
            { label: 'Installation', slug: 'installation' },
            { label: 'Quickstart', slug: 'quickstart' },
            { label: 'Running Renart', slug: 'running-renart' },
          ],
        },
        {
          label: 'Using Renart',
          items: [
            { label: 'Inspect and Materialize', slug: 'inspect-and-materialize' },
            { label: 'Visual Workflow', slug: 'visual-workflow' },
          ],
        },
      ],
    }),
  ],
});
