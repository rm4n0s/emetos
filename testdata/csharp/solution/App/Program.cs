using System;
using System.Collections.Generic;

namespace App;

public static class Program
{
    public static void Main(string[] args)
    {
        var names = new List<string>();
        names.Add(Greeter.Greet("world"));
        int count = "text";
        Console.WriteLine(names.Count + count + missing);
        Console.WriteLine(Greeter.Greet("again"));
    }
}
