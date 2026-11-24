const http = require('http');

let items = [
  { id: 1, name: "Widget A", price: 19.99, in_stock: true },
  { id: 2, name: "Widget B", price: 29.50, in_stock: false },
  { id: 3, name: "Gadget Pro", price: 99.00, in_stock: true }
];

function sendJSON(res, statusCode, data) {
  const payload = JSON.stringify(data);
  res.writeHead(statusCode, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload)
  });
  res.end(payload);
}

function parseBody(req) {
  return new Promise((resolve, reject) => {
    let body = '';
    req.on('data', chunk => { body += chunk; });
    req.on('end', () => {
      if (!body) return resolve({});
      try {
        resolve(JSON.parse(body));
      } catch (err) {
        reject(err);
      }
    });
  });
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  const pathname = url.pathname;

  // GET /health
  if (req.method === 'GET' && pathname === '/health') {
    return sendJSON(res, 200, { status: "ok", service: "items-api" });
  }

  // GET /items
  if (req.method === 'GET' && pathname === '/items') {
    return sendJSON(res, 200, items);
  }

  // GET /items/:id
  const itemMatch = pathname.match(/^\/items\/(\d+)$/);
  if (req.method === 'GET' && itemMatch) {
    const id = parseInt(itemMatch[1], 10);
    const item = items.find(i => i.id === id);
    if (!item) {
      return sendJSON(res, 404, { error: "item not found" });
    }
    return sendJSON(res, 200, item);
  }

  // POST /items
  if (req.method === 'POST' && pathname === '/items') {
    try {
      const data = await parseBody(req);
      if (!data.name || typeof data.price !== 'number') {
        return sendJSON(res, 400, { error: "invalid payload: name and price required" });
      }
      const newItem = {
        id: items.length > 0 ? Math.max(...items.map(i => i.id)) + 1 : 1,
        name: String(data.name),
        price: data.price,
        in_stock: Boolean(data.in_stock)
      };
      items.push(newItem);
      return sendJSON(res, 201, newItem);
    } catch (err) {
      return sendJSON(res, 400, { error: "malformed JSON body" });
    }
  }

  // PUT /items/:id
  if (req.method === 'PUT' && itemMatch) {
    const id = parseInt(itemMatch[1], 10);
    const index = items.findIndex(i => i.id === id);
    if (index === -1) {
      return sendJSON(res, 404, { error: "item not found" });
    }
    try {
      const data = await parseBody(req);
      if (data.name) items[index].name = String(data.name);
      if (typeof data.price === 'number') items[index].price = data.price;
      if (typeof data.in_stock === 'boolean') items[index].in_stock = data.in_stock;
      return sendJSON(res, 200, items[index]);
    } catch (err) {
      return sendJSON(res, 400, { error: "malformed JSON body" });
    }
  }

  // DELETE /items/:id
  if (req.method === 'DELETE' && itemMatch) {
    const id = parseInt(itemMatch[1], 10);
    const index = items.findIndex(i => i.id === id);
    if (index === -1) {
      return sendJSON(res, 404, { error: "item not found" });
    }
    items.splice(index, 1);
    return sendJSON(res, 200, { deleted: true, id: id });
  }

  // 404 Not Found
  sendJSON(res, 404, { error: "endpoint not found" });
});

const port = process.env.PORT ? parseInt(process.env.PORT, 10) : 3000;
server.listen(port, '127.0.0.1', () => {
  console.log(`Node items-api listening on http://127.0.0.1:${port}`);
});

module.exports = server;
