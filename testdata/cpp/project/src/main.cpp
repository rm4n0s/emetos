#include "shapes.h"

int main() {
    auto r = makeRectangle(3, 4);
    int total = area(r);
    total += missing(total);
    return total;
}
