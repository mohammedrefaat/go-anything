const test = require('node:test');
const assert = require('node:assert');
const { calculateDiscount } = require('./server');

test('calculateDiscount with SAVE10', () => {
  const result = calculateDiscount(100, 'SAVE10');
  assert.strictEqual(result, 90);
});

test('calculateDiscount without coupon', () => {
  const result = calculateDiscount(50, '');
  assert.strictEqual(result, 50);
});
