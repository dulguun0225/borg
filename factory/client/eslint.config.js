// The lint wall. Whatever the compilers do not refuse and the client profile
// forbids is a rule here, and a rule here fails the build: `npm run lint` is
// one of the four commands every change runs.
//
// The import-boundary block at the end is this client's deps.txt. One allowed
// edge per line with its reason, and an import not on the list fails.
const eslint = require('@eslint/js');
const tseslint = require('typescript-eslint');
const angular = require('angular-eslint');

// The screens: each is a directory under src/app, and the four hold the same
// file names.
const screens = ['work', 'ops', 'factory', 'people'];

module.exports = tseslint.config(
  {
    files: ['**/*.ts'],
    extends: [
      eslint.configs.recommended,
      ...tseslint.configs.strictTypeChecked,
      ...tseslint.configs.stylisticTypeChecked,
      ...angular.configs.tsAll,
    ],
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: __dirname },
    },
    processor: angular.processInlineTemplates,
    rules: {
      // Every component is standalone and every piece of state is a signal:
      // the two clauses of the profile a compiler cannot check.
      '@angular-eslint/prefer-standalone': 'error',
      '@angular-eslint/prefer-signals': 'error',
      '@angular-eslint/no-uncalled-signals': 'error',
      // Zoneless: change detection is never provided by a zone, and no
      // application file reaches for the zone's API.
      '@angular-eslint/prefer-on-push-component-change-detection': 'off',
      // Every component of the client is a screen, a section of a screen too
      // large for the 500-line bound, or the shell around them, and the class
      // name says which. Section is not a fourth screen: the design names
      // four, and a section holds no address, no subscription and no state
      // machine of its own.
      '@angular-eslint/component-class-suffix': [
        'error',
        { suffixes: ['Screen', 'Section', 'Shell'] },
      ],
      '@angular-eslint/component-selector': [
        'error',
        { type: 'element', prefix: 'factory', style: 'kebab-case' },
      ],
      '@angular-eslint/directive-selector': [
        'error',
        { type: 'attribute', prefix: 'factory', style: 'camelCase' },
      ],
      // Explicit over implicit: no reflection outside tests, and nothing
      // resolved by a string at run time.
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/consistent-type-definitions': 'off',
      '@typescript-eslint/no-extraneous-class': 'off',
      '@typescript-eslint/restrict-template-expressions': [
        'error',
        { allowNumber: true },
      ],
      // Nothing in the workspace uses inline styles or templates, so the
      // rules that would bound their length have nothing to say.
      '@angular-eslint/component-max-inline-declarations': 'off',
      '@angular-eslint/sort-keys-in-type-decorator': 'off',
      '@angular-eslint/use-component-view-encapsulation': 'off',
      '@angular-eslint/prefer-service-decorator': 'off',
    },
  },
  {
    files: ['**/*.html'],
    extends: [
      ...angular.configs.templateAll,
    ],
    rules: {
      // One language until an owner supplies another, so nothing is marked
      // for extraction and $localize is not in the profile.
      '@angular-eslint/template/i18n': 'off',
      // A signal is read by calling it, which is the whole of how state
      // reaches a template here.
      '@angular-eslint/template/no-call-expression': 'off',
      '@angular-eslint/template/cyclomatic-complexity': 'off',
      '@angular-eslint/template/conditional-complexity': 'off',
      '@angular-eslint/template/no-inline-styles': 'error',
      '@angular-eslint/template/no-positive-tabindex': 'error',
      '@angular-eslint/template/prefer-control-flow': 'error',
      '@angular-eslint/template/label-has-associated-control': 'error',
      '@angular-eslint/template/button-has-type': 'error',
      '@angular-eslint/template/attributes-order': 'off',
      '@angular-eslint/template/prefer-static-string-properties': 'off',
    },
  },
  {
    // No RxJS in application code. The framework's own use of it is a
    // transitive dependency and not an import any file here writes.
    files: ['src/app/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            { name: 'rxjs', message: 'No RxJS in application code: state is signals.' },
            { name: 'rxjs/operators', message: 'No RxJS in application code: state is signals.' },
            { name: '@angular/core/rxjs-interop', message: 'No RxJS in application code: state is signals.' },
            { name: 'zone.js', message: 'The client is zoneless.' },
            { name: '@angular/localize', message: 'One language until an owner supplies another.' },
            { name: '@angular/localize/init', message: 'One language until an owner supplies another.' },
            { name: '@angular/material', message: 'The screens are built from the factory design system alone.' },
          ],
          patterns: [
            {
              group: ['@angular/material/*', 'rxjs/*'],
              message: 'Not in the client profile.',
            },
          ],
        },
      ],
    },
  },
  {
    // api/ and state/ import nothing of the app: they are what the screens
    // are built from, and an edge back into a screen would be a cycle.
    files: ['src/app/api/**/*.ts', 'src/app/state/**/*.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              // ../../environments/version -- the one constant the build
              // fills, which api/version.ts carries on every call.
              regex: '^\\.\\.(?!/\\.\\./environments/)',
              message: 'api/ and state/ import nothing of the app.',
            },
            { group: ['rxjs', 'rxjs/*'], message: 'No RxJS in application code.' },
          ],
        },
      ],
    },
  },
  ...screens.map((screen) => ({
    // A screen imports api/ and state/ and nothing else outside its own
    // directory. No screen imports another screen: the four are independent,
    // and a link between two is a route and not an import.
    files: [`src/app/${screen}/**/*.ts`],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              // ../api/*   -- a screen reads and writes through the fetch client
              // ../state/* -- a screen declares its own state machine and the
              //               predicates its spec decides from these types
              regex: '^\\.\\.(?!/(api|state)/)',
              message: 'A screen imports api/ and state/ and nothing else outside its own directory.',
            },
            { group: ['rxjs', 'rxjs/*'], message: 'No RxJS in application code.' },
          ],
        },
      ],
    },
  })),
  ...screens.map((screen) => ({
    // A spec additionally reaches the fakes under src/testing: a fake fetch
    // and a fake EventSource are the only way to drive a screen into each
    // state it declares.
    files: [`src/app/${screen}/**/*.spec.ts`],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              // ../api/*        -- as for any file of a screen
              // ../state/*      -- as for any file of a screen
              // ../../testing/* -- the fake fetch and the fake EventSource
              regex: '^\\.\\.(?!/(api|state)/|/\\.\\./testing/)',
              message: 'A spec imports api/, state/ and the fakes under src/testing.',
            },
            { group: ['rxjs', 'rxjs/*'], message: 'No RxJS in application code.' },
          ],
        },
      ],
    },
  })),
  {
    // The shell imports the four screens' routes and nothing else of a
    // screen: what a screen renders is reached by navigating to it.
    files: ['src/app/app.ts', 'src/app/app.routes.ts', 'src/app/app.config.ts'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              // ./work/work.routes       -- the Work screen's own routes
              // ./ops/ops.routes         -- the Ops screen's own routes
              // ./factory/factory.routes -- the Factory screen's own routes
              // ./people/people.routes   -- the People screen's own routes
              regex: '^\\./(work|ops|factory|people)/(?!(work|ops|factory|people)\\.routes$)',
              message: 'The shell imports the four screens\' routes only.',
            },
            { group: ['rxjs', 'rxjs/*'], message: 'No RxJS in application code.' },
          ],
        },
      ],
    },
  },
);
