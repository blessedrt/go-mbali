// This test calls a mmblod that doesn't exist.

--> {"jsonrpc": "2.0", "id": 2, "mmblod": "invalid_mmblod", "params": [2, 3]}
<-- {"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"the mmblod invalid_mmblod does not exist/is not available"}}
