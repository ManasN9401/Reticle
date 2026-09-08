import fs from 'node:fs/promises'
import path from 'node:path'
import { createHash } from 'node:crypto'
import { findRepoRoot, isInside } from '../paths'
import type { ApiResult, HitlCheckpoint, HitlResolveRequest } from '../../src/shared/ipc'
function valid(target:string):boolean {const root=findRepoRoot();return !!root && /^approval_req_[a-f0-9]+_[0-9]+\.md$/.test(path.basename(target)) && isInside(path.join(root,'.reticle','approvals'),target)}
export async function readCheckpoint(target:string):Promise<HitlCheckpoint>{
 const empty:HitlCheckpoint={path:target,exists:false,content:'',status:'UNKNOWN',feedback:''};if(!valid(target))return empty
 try{const content=await fs.readFile(target,'utf8');let status:HitlCheckpoint['status']='PENDING',feedback='';try{const d=JSON.parse(await fs.readFile(target+'.decision.json','utf8'));status=d.decision;feedback=d.feedback??''}catch{};return {...empty,exists:true,content,status,feedback}}catch{return empty}
}
export async function resolveCheckpoint(request:HitlResolveRequest):Promise<ApiResult<HitlCheckpoint>>{
 if(!valid(request.path)||!['APPROVED','REJECTED'].includes(request.decision))return {ok:false,error:'Invalid approval request'}
 try{const content=await fs.readFile(request.path);const decision={hash:createHash('sha256').update(content).digest('hex'),decision:request.decision,feedback:request.feedback??''};await fs.writeFile(request.path+'.decision.json',JSON.stringify(decision),{flag:'wx',mode:0o600});return {ok:true,data:await readCheckpoint(request.path)}}catch{return {ok:false,error:'Approval could not be recorded or was already resolved'}}
}
