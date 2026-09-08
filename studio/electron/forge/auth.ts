import fs from 'node:fs'
import path from 'node:path'
import { findRepoRoot } from '../paths'
export function controlHeaders(): Record<string,string> {
 const root=findRepoRoot();if(!root)return {}
 try{return {Authorization:'Bearer '+fs.readFileSync(path.join(root,'.reticle','control-token'),'utf8').trim()}}catch{return {}}
}
export function requireLocalHost(host:string):void{if(!['localhost','127.0.0.1','::1'].includes(host))throw new Error('Only local Forge connections are supported')}
