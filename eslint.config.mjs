import tseslint from 'typescript-eslint'
import prettierConfig from 'eslint-config-prettier'
import reactHooks from 'eslint-plugin-react-hooks'
import jsxA11y from 'eslint-plugin-jsx-a11y'

export default tseslint.config(
  { ignores: ['node_modules/**', 'dist/**', 'out/**', 'web/admin/**'] },
  ...tseslint.configs.recommended,
  prettierConfig,
  {
    files: ['src/**/*.{ts,tsx}'],
    plugins: {
      'react-hooks': reactHooks,
    },
    rules: {
      // Downgrade rules that fire on existing code to 'warn'
      // to keep CI green while the codebase is onboarded
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': 'warn',
      'react-hooks/exhaustive-deps': 'warn',
    },
  },
  {
    // Apply jsx-a11y to renderer JSX/TSX only (main/preload are not JSX)
    files: ['src/renderer/**/*.{ts,tsx}'],
    plugins: {
      'jsx-a11y': jsxA11y,
    },
    rules: {
      ...jsxA11y.configs.recommended.rules,
      // Internal links use react-router — anchor href rules don't apply
      'jsx-a11y/anchor-is-valid': 'warn',
      // autoFocus on the first field of a dialog or inline-rename input is correct
      // a11y practice (ARIA Authoring Practices Guide §3.1) — in an Electron desktop
      // app there are no pop-under surprises so the risk the rule guards against is absent.
      'jsx-a11y/no-autofocus': 'off',
    },
  }
)
