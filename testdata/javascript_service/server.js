const http = require('http');

function calculateDiscount(price, coupon) {
  if (coupon === 'SAVE10') {
    return price * 0.9;
  }
  if (coupon === 'HALF') {
    return price * 0.5;
  }
  return price;
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  if (req.method === 'GET' && url.pathname === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    return res.end(JSON.stringify({ status: 'healthy', runtime: 'javascript' }));
  }
  if (req.method === 'GET' && url.pathname === '/price') {
    const rawPrice = parseFloat(url.searchParams.get('amount') || '0');
    const coupon = url.searchParams.get('coupon') || '';
    const finalPrice = calculateDiscount(rawPrice, coupon);
    res.writeHead(200, { 'Content-Type': 'application/json' });
    return res.end(JSON.stringify({ original: rawPrice, discount_applied: coupon !== '', total: finalPrice }));
  }
  res.writeHead(404, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ error: 'not found' }));
});

if (require.main === module) {
  const port = process.env.PORT || 3000;
  server.listen(port, '127.0.0.1', () => {
    console.log(`JavaScript fixture listening on http://127.0.0.1:${port}`);
  });
}

module.exports = { server, calculateDiscount };
