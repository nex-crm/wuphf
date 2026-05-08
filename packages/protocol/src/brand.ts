declare const brand: unique symbol;

export type Brand<T, B extends string> = T & { readonly [brand]: B };

export type Brand2<T, B1 extends string, B2 extends string> = T & {
  readonly [brand]: B1 | B2;
};
