const lstackSingTapdBug = require('../lstack-single-tapd-bug')

it("当参数 when 不为 'always' 时返回 'true'", () => {
  const [actual] = lstackSingTapdBug({}, 'never')
  expect(actual).toBe(true)
})

describe("当参数 when 为 'always' 时：", () => {
  const when = 'always'

  test("当包含不是纯数值的 issue 或 tapd ID 时，返回 'false'", () => {
    const references = [{ issue: '123c' }]
    const [actual] = lstackSingTapdBug({ references }, when)
    expect(actual).toBe(false)
  })

  test("当包含两个 tapd ID 时，返回 'false'", () => {
    const references = [{ issue: '1000001' }, { issue: '1000002' }]
    const [actual] = lstackSingTapdBug({ references }, when)
    expect(actual).toBe(false)
  })

  test("当只包含一个 tapd ID 时，返回 'true'", () => {
    const references = [{ issue: '1000001' }]
    const [actual] = lstackSingTapdBug({ references }, when)
    expect(actual).toBe(true)
  })

  test("当只包含一个 issue ID 时，返回 'true'", () => {
    const references = [{ issue: '11' }]
    const [actual] = lstackSingTapdBug({ references }, when)
    expect(actual).toBe(true)
  })

  test("当包含两个 issue ID 时，返回 'true'", () => {
    const references = [{ issue: '11' }]
    const [actual] = lstackSingTapdBug({ references }, when)
    expect(actual).toBe(true)
  })

  test("当包含一个 issue ID 和 一个 tapd ID 时，返回 'true'", () => {
    const references = [{ issue: '11' }, { issue: '1000001' }]
    const [actual] = lstackSingTapdBug({ references }, when)
    expect(actual).toBe(true)
  })
})
