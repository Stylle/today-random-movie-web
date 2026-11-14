const test=require('node:test'),assert=require('node:assert/strict');
test('completed sync clears stale error without replacing unsaved connection credentials',async()=>{
 const {applySyncMetadata,currentSyncError}=await import('../frontend/src/sync-status.js');
 const settings={fnServer:'http://draft.example',fnUsername:'draft',fnPassword:'unsaved-fixture',lastSyncError:'old HTTP 503'};
 assert.equal(currentSyncError({running:true},settings),'');
 assert.equal(currentSyncError({running:false,finishedAt:'now',error:''},settings),'');
 assert.equal(currentSyncError({running:false,finishedAt:'now',error:'current failure'},settings),'current failure');
 assert.equal(currentSyncError({running:false},settings),'old HTTP 503');
 applySyncMetadata(settings,{lastSyncAt:'now',lastSyncSuccessAt:'now',lastSyncError:'',fnPassword:''});
 assert.equal(settings.fnPassword,'unsaved-fixture');assert.equal(settings.fnServer,'http://draft.example');assert.equal(settings.lastSyncError,'');assert.equal(settings.lastSyncSuccessAt,'now');
});
