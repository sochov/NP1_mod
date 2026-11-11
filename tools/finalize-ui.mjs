import {writeFile,mkdir} from 'node:fs/promises';
const directory=new URL('../backend/internal/webui/dist/',import.meta.url);
await mkdir(directory,{recursive:true});
await writeFile(new URL('.gitkeep',directory),'');
