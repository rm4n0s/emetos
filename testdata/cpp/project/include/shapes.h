#pragma once

struct Rectangle {
    int width;
    int height;
};

Rectangle makeRectangle(int width, int height);
int area(const Rectangle &r);
