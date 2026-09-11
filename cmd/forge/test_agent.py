import os
import subprocess

if not os.environ.get('GROQ_API_KEY'):
    raise RuntimeError('Set GROQ_API_KEY in the process environment before running this manual test')
p = subprocess.Popen(['python', r'd:\Reticle\cmd\forge\workspaces\forge_workspace_20260806_095140\workers\game-designer-agent.py'], 
                     stdin=subprocess.PIPE, 
                     stdout=subprocess.PIPE, 
                     stderr=subprocess.PIPE, 
                     text=True)
out, err = p.communicate('{"id":"test"}\n')
print('OUT:', out)
print('ERR:', err)
