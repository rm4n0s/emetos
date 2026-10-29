// Package resultgrid is the grid that the three database extensions show their results in (SPEC §10.21 "The results panel",
// §10.24): the rows of a SQL result set, the documents of MongoDB in table form, the fields of a Redis hash. It is made to stay fast
// with large results: only the rows in view are built and laid out (a sliver list of rows of one height), and the rows are read
// from a Source on demand, so a result of 100,000 rows is never copied into widgets, and a Source that makes its rows when asked
// (NewFuncSource) is never held in memory whole.
//
// The grid has columns the user can resize, a sort on the client (a click on a header: ascending, descending, none), a selection of
// cells, rows, columns or everything with the mouse and the keyboard, copying as text and as TSV (the cell, the row, the column, the
// selection, with or without the header), NULL shown as such, long values cut with their whole text in a hover, binary shown as hex,
// dates in ISO form, paging (a footer that tells how many rows there are and offers "Fetch More" while there are more), and the
// export of the result to CSV and JSON.
//
// # The parts
//
// Value is a cell and Source gives the rows. Model is the grid without its widgets: the order of the rows, the sort, the selection,
// the widths of the columns, the text of a copy and the export; it is tested without a UI. Grid is the widget over a Model; it is
// used on the UI thread only, like every widget.
package resultgrid
