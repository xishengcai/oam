/* eslint-disable global-require */
module.exports = {
  rules: {
    'lstack-breaking-change-attention': require('./lib/rules/lstack-breaking-change-attention'),
    'lstack-header-max-length': require('./lib/rules/lstack-header-max-length'),
    'lstack-release-branch-type-valid': require('./lib/rules/lstack-release-branch-type-valid'),
    'lstack-scope-empty': require('./lib/rules/lstack-scope-empty'),
    'lstack-scope-enum': require('./lib/rules/lstack-scope-enum'),
    'lstack-single-tapd-bug': require('./lib/rules/lstack-single-tapd-bug'),
    'lstack-subject-action-enum': require('./lib/rules/lstack-subject-action-enum'),
    'lstack-subject-action-valid': require('./lib/rules/lstack-subject-action-valid'),
    'lstack-subject-full-stop': require('./lib/rules/lstack-subject-full-stop'),
  },
}
