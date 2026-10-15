---
name: verification-loop
description: How a worker proves its files work before it finishes, using only the tools it really has.
---

# Verification loop

Finish only when every file you were asked to produce has been checked. A file you wrote but did not read back is unverified.

1. **Read back, completely.** Call `read_file` on each file you created or changed. Listing a directory, or running `ls` or `cat`, does not count. A long file comes back in parts: if the reply starts with `[reticle: lines ... of ...]`, call `read_file` again with the `offset` it names until you have seen the end. Fix what you find, then read the file again.
2. **Check the pieces fit.** Every name one file takes from another must exist there with the same spelling: script and stylesheet paths in HTML, imported and exported names, data keys, asset file names. Use `find_files` to see what exists and `search_codebase` to find where a name is used.
3. **Make fixes small.** Change the part that is wrong with `replace_file_content`, or several parts of one file with `apply_edits`, passing only the changed text. Never "replace" a small piece with a whole new file: that nests a second copy inside the first. To rewrite a whole file, read it completely first, then call `write_file` with `overwrite=true`.
4. **Web output.** After your last edit, call `check_web_page`, fix every problem it lists, and run it again until it reports none. If you also have `run_page`, call it after that and fix every exception, failed request and blank-screen verdict it reports.
5. **Commands.** If you have `execute_terminal_command` and the project defines a build, test or lint command (for example `python -m pytest` or `python -m py_compile file.py`), run it. A failing command means the task is not done. Do not invent commands the project does not define. The terminal is a Linux container with Python and pip but no node, npm or git, so a JavaScript check has to be `check_web_page` or `run_page` rather than a node command.
6. **Report.** Call `mark_task_complete` with what you produced, what you checked and how, and anything you could not verify. State a failure plainly instead of claiming success.
