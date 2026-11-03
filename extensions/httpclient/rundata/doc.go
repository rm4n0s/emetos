// Package rundata reads the data files of the collection runner (SPEC §10.33 "The collection runner"): a CSV file with a header row, or a
// JSON array of objects. Each row is one iteration of a run and its columns are variables of that iteration. The values are text: a
// number or a boolean of a JSON file is kept as it is written, a null is the empty text, an object or an array is compact JSON text.
package rundata
