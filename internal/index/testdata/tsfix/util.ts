export function greet(name: string): string {
  return "hi " + name;
}

// Object-literal method with an expression body that calls greet.
export const api = {
  hi: (n: string) => greet(n),
};
