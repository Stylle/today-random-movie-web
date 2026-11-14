const test=require('node:test'),assert=require('node:assert/strict');
test('failed logout keeps session state; success or expiry clears it',async()=>{
 const {store,logout}=await import('../frontend/src/store.js');
 store.user={username:'fixture'};
 global.fetch=async()=>new Response(JSON.stringify({detail:'offline'}),{status:503,headers:{'Content-Type':'application/json'}});
 await assert.rejects(logout());assert.equal(store.user.username,'fixture');
 global.fetch=async()=>{throw new TypeError('offline')};await assert.rejects(logout());assert.equal(store.user.username,'fixture');
 global.fetch=async()=>new Response('{}',{status:200,headers:{'Content-Type':'application/json'}});await logout();assert.equal(store.user,null);
 store.user={username:'fixture'};global.fetch=async()=>new Response('{}',{status:401,headers:{'Content-Type':'application/json'}});await logout();assert.equal(store.user,null);
});
