import { describe, it, expect } from 'vitest'
import { formatRecordedCost, signedStockDelta } from '../inventory'

describe('signedStockDelta', () => {
  it('sends receipts as a positive delta', () => {
    expect(signedStockDelta('receipt', '5')).toBe(5)
    expect(signedStockDelta('receipt', '-5')).toBe(5)
  })

  it('sends consumption as a negative delta, like job consumption', () => {
    expect(signedStockDelta('consumption', '5')).toBe(-5)
    expect(signedStockDelta('consumption', '-5')).toBe(-5)
  })

  it('keeps the sign the user typed for adjustments', () => {
    expect(signedStockDelta('adjustment', '-2.5')).toBe(-2.5)
    expect(signedStockDelta('adjustment', '3')).toBe(3)
  })

  it('refuses empty, zero and non-numeric amounts', () => {
    expect(signedStockDelta('receipt', '')).toBeNull()
    expect(signedStockDelta('receipt', '0')).toBeNull()
    expect(signedStockDelta('consumption', 'abc')).toBeNull()
  })
})

describe('formatRecordedCost', () => {
  it('formats with the recorded currency', () => {
    expect(formatRecordedCost(250, 'MXN')).toEqual({
      text: 'MX$250.00',
      currencyKnown: true,
    })
    expect(formatRecordedCost(12.5, 'usd').text).toBe('$12.50')
  })

  it('never assumes a currency that was not recorded', () => {
    expect(formatRecordedCost(250, '')).toEqual({ text: '250.00', currencyKnown: false })
    expect(formatRecordedCost(250, null)).toEqual({ text: '250.00', currencyKnown: false })
    expect(formatRecordedCost(250, 'XX')).toEqual({ text: '250.00', currencyKnown: false })
  })
})
