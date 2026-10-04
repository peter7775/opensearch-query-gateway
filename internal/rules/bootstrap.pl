% bootstrap.pl — rule base načítaná rule enginem při startu služby.
% Udržujte byznysová pravidla zde, oddělená od Go kódu, aby bylo možné
% je měnit a verzovat nezávisle na buildu binárky.
%
% Engine se ptá na tyto predikáty:
%   field_alias(+Raw, -Canonical)          normalizace názvu pole
%   valid_clause(+Field, +Op, +Value)      guard nad jednou klauzulí
%   rejection_reason(+Field, +Op, +Value, -Reason)  (volitelné) důvod odmítnutí
%   known_field(+Field)                    allow-list polí (i pro řazení)
%
% Op je jedno z: eq (shoda/fráze/wildcard/fuzzy), range ([a TO b], >, >=, <, <=,
% relativní čas last:15m).
%
% Vedle ručně psaných faktů lze načíst i fakta vygenerovaná nástrojem
% schema-export (field/2, field_type/3, …) — viz rules.schema_files v configu.
% Pole ze schématu se pak automaticky stávají známými a typově se kontrolují.

% --- Aliasy polí ---------------------------------------------------------
% field_alias(SurovyNazev, KanonickyNazev).
field_alias(author, created_by).
field_alias(tag, tags).
field_alias(ts, '@timestamp').

% --- Allow-list polí, na které je dovoleno se dotazovat ------------------
known_field(created_by).
known_field(tags).
known_field(title).
known_field(status).
known_field(published_at).
known_field(views).
known_field('@timestamp').

% Pole z vygenerovaného schématu (schema-export) jsou známá automaticky.
% Objekty se dotazují přes podpole; pole uvnitř nested struktur by vyžadovala
% nested query, kterou builder zatím negeneruje, proto je nepovolujeme.
known_field(F) :- field(_, F), \+ object(_, F), \+ nested(_, F), \+ inside_nested(F).

inside_nested(F) :-
    nested(_, P),
    atom_concat(P, '.', Prefix),
    atom_concat(Prefix, _, F).

numeric_type(long).
numeric_type(integer).
numeric_type(short).
numeric_type(byte).
numeric_type(double).
numeric_type(float).
numeric_type(half_float).
numeric_type(scaled_float).
numeric_type(unsigned_long).

numeric_field(views).
numeric_field(F) :- field_type(_, F, T), numeric_type(T).

date_field(published_at).
date_field('@timestamp').
date_field(F) :- field_type(_, F, date).
date_field(F) :- field_type(_, F, date_nanos).

% Rozsahové dotazy dávají smysl jen nad čísly, daty, IP a keywordy.
range_field(F) :- numeric_field(F).
range_field(F) :- date_field(F).
range_field(F) :- field_type(_, F, ip).
range_field(F) :- field_type(_, F, keyword).

% --- Validace jednotlivé klauzule ----------------------------------------
% valid_clause(Field, Op, Value) uspěje, pokud je klauzule povolená.
valid_clause(Field, eq, _Value) :-
    known_field(Field), !.

valid_clause(Field, range, _Value) :-
    known_field(Field),
    range_field(Field), !.

% --- Důvody odmítnutí (pro čitelné chybové hlášky) -----------------------
rejection_reason(Field, _Op, _Value, 'fields inside nested objects are not supported yet') :-
    ( nested(_, Field) ; inside_nested(Field) ), !.

rejection_reason(Field, _Op, _Value, 'unknown field') :-
    \+ known_field(Field), !.

rejection_reason(Field, range, _Value, 'range queries require a numeric or date field') :-
    known_field(Field),
    \+ range_field(Field), !.
