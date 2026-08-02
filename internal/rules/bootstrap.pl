% bootstrap.pl — rule base načítaná rule enginem při startu služby.
% Udržujte byznysová pravidla zde, oddělená od Go kódu, aby bylo možné
% je měnit a verzovat nezávisle na buildu binárky.

% --- Aliasy polí ---------------------------------------------------------
% field_alias(SurovyNazev, KanonickyNazev).
field_alias(author, created_by).
field_alias(tag, tags).

% --- Allow-list polí, na které je dovoleno se dotazovat ------------------
known_field(created_by).
known_field(tags).
known_field(title).
known_field(published_at).

numeric_field(published_at).

% --- Validace jednotlivé klauzule ----------------------------------------
% valid_clause(Field, Op, Value) uspěje, pokud je klauzule povolená.
valid_clause(Field, _Op, _Value) :-
    known_field(Field).

valid_clause(Field, range, _Value) :-
    numeric_field(Field).
