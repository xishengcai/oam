function isRevertTpe(type = null) {
  if (type === null) {
    return false
  }
  return type.toLowerCase() === 'revert'
}

module.exports = isRevertTpe
