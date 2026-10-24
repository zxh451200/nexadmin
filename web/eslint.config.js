import tseslint from 'typescript-eslint'
import reactHooks from 'eslint-plugin-react-hooks'
import globals from 'globals'

// 约束类检查默认只警告、不阻断构建（设计文档 §8 / §15）
const uiPrimitivesInPages = [
  '@/framework/ui/table',
  '@/framework/ui/dialog',
  '@/framework/ui/sheet',
  '@/framework/ui/drawer',
]

export default tseslint.config(
  { ignores: ['dist', 'node_modules'] },
  ...tseslint.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: { globals: globals.browser },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
      // 颜色只能用语义 token：写死色值、具体色阶类给警告
      'no-restricted-syntax': [
        'warn',
        { selector: 'Literal[value=/^#[0-9a-fA-F]{3,8}$/]', message: '颜色请用语义 token（bg-primary 等），不要写死色值' },
        {
          selector: 'Literal[value=/\\b(bg|text|border|ring|fill|stroke)-(red|blue|green|yellow|orange|purple|pink|gray|slate|zinc|neutral|stone|sky|cyan|teal|emerald|lime|amber|indigo|violet|fuchsia|rose)-[0-9]{2,3}\\b/]',
          message: '颜色请用语义 token，不要用具体色阶类',
        },
      ],
    },
  },
  {
    // 页面与内置模块页面：直接使用 table / dialog 等原语时提示改用框架组件
    files: ['src/pages/**/*.{ts,tsx}', 'src/framework/modules/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'warn',
        { patterns: uiPrimitivesInPages.map((p) => ({ group: [p], message: '请改用 DataTable / ResponsiveDialog 等框架组件' })) },
      ],
    },
  },
)
