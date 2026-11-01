import { greet } from "emetoslib";
import { add } from "./helpers";

const total: number = add(1, 2);
const bad: string = total;
console.log(greet("world"), bad, missing_name);
