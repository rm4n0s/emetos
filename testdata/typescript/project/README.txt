The project of the tests of the TypeScript and JavaScript pack against the real typescript-language-server (build tag emetos_tsls).
The tests copy it, add node_modules/emetoslib (a package with a declaration file and a source) and, for the workspace version,
node_modules/typescript, and run the server in the copy.
