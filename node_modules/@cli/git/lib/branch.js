const { execSync } = require('child_process')

/**
 * 获取本地当前所在 git 分支名称
 */
function getCurrentBranch() {
  try {
    const gitStatusLog = execSync('git status', { encoding: 'utf8' })
    const matchedArray = gitStatusLog.match(/^On branch (.+)\s/)
    if (matchedArray instanceof Array && matchedArray.length >= 2) {
      return matchedArray[1]
    }
  } catch (err) {
    throw new Error(`执行 git 命令时出错，具体如下：\n${err.toString()}`)
  }
  return ''
}

module.exports.getCurrentBranch = getCurrentBranch
