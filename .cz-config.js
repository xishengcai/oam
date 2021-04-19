const chalk = require('chalk')

// 通过环境变量中的 HUSKY_GIT_PARAMS 参数判断是否为 Commitlint 时使用的此配置
const isCommitlintEnv = !!process.env.HUSKY_GIT_PARAMS

// 如果是 Commitlint 而非 git-cz 工具触发的校验，则无需提示
if (!isCommitlintEnv) {
  const hint =
    '注意：如果本次提交包含不兼容的变更`BREAKING CHANGE`，请不要使用本工具进行提交，\n' +
    '根据 LStack 前端提交规范，需要手动在类型/作用域前缀之后，`:`之前，附加`!`字符，以进一步提醒注意破坏性变更。\n' +
    '如：`fix(lcs)!: 这是 subject`。链接：http://frontend.lstack-inner.com/guidelines/convention/commit-message.html'
  console.log(chalk.keyword('orange')(hint))
}

const config = require('@cli/commitizen-config-lstack')

module.exports = {...config,  scopes: ['root']}

