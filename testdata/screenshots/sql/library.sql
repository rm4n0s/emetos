-- The database of the SQL shot of README.md: scripts/screenshots.sh makes library.db from this file.
CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL, born INTEGER);
CREATE TABLE books (id INTEGER PRIMARY KEY, author_id INTEGER REFERENCES authors(id), title TEXT NOT NULL, year INTEGER);
INSERT INTO authors VALUES (1, 'Ursula K. Le Guin', 1929), (2, 'Stanislaw Lem', 1921), (3, 'Octavia E. Butler', 1947);
INSERT INTO books VALUES
  (1, 1, 'The Dispossessed', 1974),
  (2, 1, 'A Wizard of Earthsea', 1968),
  (3, 2, 'Solaris', 1961),
  (4, 3, 'Kindred', 1979),
  (5, 2, 'The Cyberiad', 1965);
