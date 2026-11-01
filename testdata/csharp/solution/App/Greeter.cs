namespace App;

/// <summary>Says hello.</summary>
public static class Greeter
{
    /// <summary>Says hello to <paramref name="name"/>.</summary>
    public static string Greet(string name)
    {
        return "hello " + name;
    }
}
