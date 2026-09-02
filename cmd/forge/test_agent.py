import os
import subprocess

os.environ['GROQ_API_KEY'] = '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'
p = subprocess.Popen(['python', r'd:\Reticle\cmd\forge\workspaces\forge_workspace_20260806_095140\workers\game-designer-agent.py'], 
                     stdin=subprocess.PIPE, 
                     stdout=subprocess.PIPE, 
                     stderr=subprocess.PIPE, 
                     text=True)
out, err = p.communicate('{"id":"test"}\n')
print('OUT:', out)
print('ERR:', err)
