# The Emetos Tutor

Welcome. This is a practice copy made just for you: change it, break it, nothing here matters. Open the tutor
again (Help > Emetos Tutor, or `emetos --tutor`) and you get a fresh copy.

Each lesson explains a little and then gives you exercise lines. A line marked `--->` is the one to change;
make it look like the line under it, marked `ok>`. Nothing checks your work: compare the two lines yourself.

The keys below are your own: if you changed a key binding, the tutor shows yours.

## Lesson 1: Moving around

Move the cursor with the arrow keys. {{key:cursorWordEndRight}} jumps a word to the right and
{{key:cursorWordLeft}} a word to the left. {{key:cursorHome}} goes to the start of the line and {{key:cursorEnd}}
to its end. {{key:cursorTop}} goes to the top of the file and {{key:cursorBottom}} to the bottom.
{{key:workbench.action.gotoLine}} asks for a line number and goes there.

Go to the end of this file, then come back here with {{key:workbench.action.gotoLine}}.

---> The cat sat on the mat.mat
ok>  The cat sat on the mat.

## Lesson 2: Editing lines

{{key:editor.action.deleteLines}} deletes the line of the cursor. {{key:editor.action.moveLinesUpAction}} and
{{key:editor.action.moveLinesDownAction}} move it up and down. {{key:editor.action.copyLinesDownAction}} copies it
below itself.

Delete the line that does not belong:

---> one
---> this line does not belong
---> two
ok>  one
ok>  two

Put the lines in order by moving the second one up:

---> second
---> first
ok>  first
ok>  second

Now open `practice/animals.yaml` (it is in the Explorer on the left) and follow the comments in it: you will
comment out a line with {{key:editor.action.commentLine}}.

## Lesson 3: Undo and redo

{{key:undo}} takes back what you did, again and again. {{key:redo}} does it again. Undo works per file and
survives saving.

Delete the next line with {{key:editor.action.deleteLines}}, then bring it back with {{key:undo}}:

---> I am still here.
ok>  I am still here.

## Lesson 4: Multiple cursors

Put the cursor on a word and press {{key:editor.action.addSelectionToNextFindMatch}}: the word is selected.
Press it again and the next same word is selected too, with a cursor of its own. Now type: every cursor types.
{{key:editor.action.selectHighlights}} selects every same word at once. {{key:editor.action.insertCursorBelow}}
adds a cursor on the line below, and Alt+click adds one where you click. Escape goes back to one cursor.

Change every "dog" to "cat" with multiple cursors:

---> dog, dog and one more dog
ok>  cat, cat and one more cat

Add a dash at the start of the three lines at once:

---> red
---> green
---> blue
ok>  - red
ok>  - green
ok>  - blue

## Lesson 5: Find and replace

{{key:actions.find}} opens the find box: type and Enter goes from match to match. {{key:editor.action.startFindReplaceAction}}
also opens the replace box. Escape closes them.

Replace "colour" with "color" in the whole file, then check these lines:

---> The colour of the sky is blue.
ok>  The color of the sky is blue.

## Lesson 6: Quick Open and the Command Palette

{{key:workbench.action.quickOpen}} opens any file of the folder by a part of its name: type `fru` and press
Enter to open `practice/fruits.txt`. Add a fruit at the end of it.

{{key:workbench.action.showCommands}} opens the Command Palette: every command of Emetos, with its key. Type
`sort` and run "Sort Lines Ascending" on the lines below after selecting them:

---> pear
---> apple
---> fig
ok>  apple
ok>  fig
ok>  pear

## Lesson 7: Search in all files

{{key:workbench.action.findInFiles}} searches every file of the folder. Somewhere in `practice/` a file holds a
secret word next to the word "needle". Find it and write it here:

---> The secret word is: ?
ok>  The secret word is: (the word you found)

## Lesson 8: Editor groups and tabs

{{key:workbench.action.splitEditor}} splits the editor: the same file side by side. {{key:workbench.action.focusFirstEditorGroup}}
and {{key:workbench.action.focusSecondEditorGroup}} go to the first and the second group. The **+** at the end of
the tabs of a group opens a new text file or a terminal in that group. {{key:workbench.action.closeActiveEditor}}
closes a tab.

Split this file, scroll the other side to Lesson 1, then close the other side.

## Lesson 9: The terminal

{{key:workbench.action.terminal.toggleTerminal}} shows the terminal at the bottom (and hides it again). It runs your
shell in this folder. Type `ls` and Enter: you see `TUTOR.md` and `practice`. The terminal can also be an editor
tab: the **+** of the tabs, "New Terminal".

## Lesson 10: Settings and keys

{{key:workbench.action.openSettings}} opens the settings. Search for "font size" and change it; the change is
saved at once. {{key:workbench.action.openGlobalKeybindings}} lists every key: search for a command and give it
the key you like. Open the tutor again and it shows your new keys.

## Lesson 11: What next

- {{key:workbench.extensions.action.showInstalledExtensions}} shows the extensions: Git, the terminal, the
  languages, the databases and more, each one can be turned off.
- The README of Emetos tells what is built and how to set up language servers and debuggers.
- Help > Emetos Tutor opens a fresh copy of these lessons whenever you want.

You made it to the end. Happy editing!
