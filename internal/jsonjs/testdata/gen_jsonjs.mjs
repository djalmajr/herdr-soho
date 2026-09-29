import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const cases = [
  {type:'string',s:'"\\\b\f\n\r\t\u0000\u001f <>&\u2028\u2029😀'},
  {type:'number',n:-0},{type:'number',n:1e21},{type:'number',n:1e20},{type:'number',n:1e-7},{type:'number',n:1e-6},{type:'number',special:'NaN',n:NaN},{type:'number',special:'Infinity',n:Infinity},
  {type:'undefined'},
  {type:'array',values:[1,'á😀',null,true,'__UNDEFINED__']},
  {type:'object',pairs:[['z',1],['10','ten'],['2','two'],['a','__UNDEFINED__'],['x','😀']]},
];
const rows=cases.map(d=>{let v; if(d.type==='string')v=d.s;else if(d.type==='number')v=d.n;else if(d.type==='undefined')v=undefined;else if(d.type==='array')v=d.values.map(x=>x==='__UNDEFINED__'?undefined:x);else v=Object.fromEntries(d.pairs.map(([k,x])=>[k,x==='__UNDEFINED__'?undefined:x]));return {kind:'stringify',value:d,compact:JSON.stringify(v)??'',indent:JSON.stringify(v,null,2)??''};});
for(const source of ['{"b":1,"10":2,"2":3,"a":[true,null,1.5]}','{"01":1,"4294967294":2,"4294967295":3,"x":4}','["á😀",-0,1e21,1e-7]','1e400','["\\ud800","\\udc00","\\ud83d\\ude00"]']){const v=JSON.parse(source);rows.push({kind:'parse',source,compact:JSON.stringify(v),keys:v&&typeof v==='object'&&!Array.isArray(v)?Object.keys(v):[]});}
fs.writeFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)),'jsonjs.json'),JSON.stringify(rows,null,2)+'\n');
