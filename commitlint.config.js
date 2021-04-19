module.exports = {
    extends: ['@cli/commitlint-config-lstack'],
    plugins: ['@cli/lstack'],
    rules: {
        'lstack-scope-enum': [2, 'always', ['root']],}
}
