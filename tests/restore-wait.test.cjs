const {test}=require('node:test'),assert=require('node:assert/strict');
test('restore readiness requires old session invalidation, not just a healthy server',async()=>{
 const {waitForRestoredSession}=await import('../frontend/src/restore-wait.mjs');
 const controller=new AbortController();global.fetch=async()=>new Response('{}',{status:401});assert.equal(await waitForRestoredSession('/me',controller.signal),true);
 global.fetch=async()=>new Response(JSON.stringify({authenticated:false}),{status:200});assert.equal(await waitForRestoredSession('/session',controller.signal),true);
 controller.abort();assert.equal(await waitForRestoredSession('/me',controller.signal),false);
 await assert.rejects(waitForRestoredSession('/me',new AbortController().signal,0),/勿重复导入/);
});
