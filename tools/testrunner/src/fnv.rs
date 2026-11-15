//! FNV-1a over 128 bits. It gives the same key on every machine and with
//! every Rust release, which std's hashers do not promise, so a key one run
//! writes is the key the next run reads.

const OFFSET: u128 = 0x6c62_272e_07bb_0142_62b8_2175_6295_c58d;
const PRIME: u128 = 0x0000_0000_0100_0000_0000_0000_0000_013b;

#[derive(Clone)]
pub struct Fnv(u128);

impl Default for Fnv {
    fn default() -> Self {
        Fnv(OFFSET)
    }
}

impl Fnv {
    pub fn bytes(&mut self, b: &[u8]) -> &mut Fnv {
        for &x in b {
            self.0 ^= u128::from(x);
            self.0 = self.0.wrapping_mul(PRIME);
        }
        self
    }

    /// Adds s and then a zero byte, so that ("ab", "c") and ("a", "bc")
    /// hash apart.
    pub fn field(&mut self, s: &str) -> &mut Fnv {
        self.bytes(s.as_bytes()).bytes(&[0])
    }

    pub fn hex(&self) -> String {
        format!("{:032x}", self.0)
    }
}

/// The hex key of the fields, in order.
pub fn key(fields: &[&str]) -> String {
    let mut h = Fnv::default();
    for f in fields {
        h.field(f);
    }
    h.hex()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn known_values() {
        // FNV-1a 128 of "" is the offset basis; of "a", the published value.
        assert_eq!(Fnv::default().hex(), "6c62272e07bb014262b821756295c58d");
        assert_eq!(
            Fnv::default().bytes(b"a").hex(),
            "d228cb696f1a8caf78912b704e4a8964"
        );
    }

    #[test]
    fn fields_do_not_run_together() {
        assert_ne!(key(&["ab", "c"]), key(&["a", "bc"]));
        assert_eq!(key(&["ab", "c"]), key(&["ab", "c"]));
    }
}
