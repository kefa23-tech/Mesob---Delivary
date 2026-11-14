import boundaries from 'eslint-plugin-boundaries';
import tsParser from '@typescript-eslint/parser';

export default [{ files: ['**/*.{ts,tsx}'], languageOptions: { parser: tsParser }, plugins: { boundaries }, settings: { 'boundaries/elements': [
  { type: 'domain', pattern: 'src/domain/*' },
  { type: 'application', pattern: 'src/application/*' },
  { type: 'infrastructure', pattern: 'src/infrastructure/*' },
  { type: 'ui', pattern: 'src/ui/*' },
  { type: 'app', pattern: 'src/app/*' },
] }, rules: { 'boundaries/element-types': ['error', { default: 'disallow', rules: [
  { from: 'domain', allow: ['domain'] },
  { from: 'application', allow: ['domain', 'application'] },
  { from: 'infrastructure', allow: ['domain', 'application', 'infrastructure'] },
  { from: 'ui', allow: ['domain', 'application', 'ui'] },
  { from: 'app', allow: ['domain', 'application', 'infrastructure', 'ui', 'app'] },
] }] } }];
